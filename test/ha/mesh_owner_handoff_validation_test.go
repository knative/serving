//go:build e2e

/*
Copyright 2026 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ha

import (
	"context"
	"encoding/json"
	"testing"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
	"knative.dev/serving/pkg/autoscaler/bucket"
)

func TestMeshBucketValidation(t *testing.T) {
	for _, fault := range []string{"", "missing-ref", "stale-ref-same-ip", "wrong-ip", "deleting-owner", "missing-bucket"} {
		t.Run(fault, func(t *testing.T) {
			const ns, ip = "system", "10.0.0.1"
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: ns}, Status: corev1.PodStatus{PodIP: ip}}
			if fault == "deleting-owner" {
				now := metav1.Now()
				pod.DeletionTimestamp = &now
			}
			objects := []runtime.Object{pod}
			for i := uint32(0); i < 10; i++ {
				name := bucket.AutoscalerBucketName(i, 10)
				objects = append(objects, &coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: coordinationv1.LeaseSpec{HolderIdentity: ptr.To("owner_" + ip)}})
				if fault == "missing-bucket" && i == 0 {
					continue
				}
				ep := discoveryv1.Endpoint{Addresses: []string{ip}, TargetRef: &corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Namespace: ns, Name: "owner"}}
				if i == 0 {
					switch fault {
					case "missing-ref":
						ep.TargetRef = nil
					case "stale-ref-same-ip":
						ep.TargetRef.Name = "old-owner"
					case "wrong-ip":
						ep.Addresses[0] = "10.0.0.2"
					}
				}
				objects = append(objects, &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Endpoints: []discoveryv1.Endpoint{ep}})
			}
			owners, err := meshBucketOwners(context.Background(), kubefake.NewSimpleClientset(objects...), ns)
			if fault == "" {
				if err != nil || len(owners) != 10 {
					t.Fatalf("valid state: %v %v", owners, err)
				}
			} else if err == nil {
				t.Fatal("accepted " + fault)
			}
		})
	}
}

func TestMeshEndpointValidation(t *testing.T) {
	const ns = "system"
	owners := map[string]string{}
	for i := uint32(0); i < 10; i++ {
		owners[bucket.AutoscalerBucketName(i, 10)] = "owner_10.0.0.1"
	}
	for _, fault := range []string{"", "missing-tls", "blank-workload", "wrong-ip", "missing-bucket"} {
		t.Run(fault, func(t *testing.T) {
			configs := []any{}
			for name := range owners {
				if fault == "missing-bucket" && name == bucket.AutoscalerBucketName(0, 10) {
					continue
				}
				meta := map[string]any{"envoy.transport_socket_match": map[string]any{"tlsMode": "istio"}, "istio": map[string]any{"workload": "autoscaler;system;knative-serving;devel;Kubernetes"}}
				ip := "10.0.0.1"
				if fault == "missing-tls" {
					delete(meta, "envoy.transport_socket_match")
				}
				if fault == "blank-workload" {
					meta["istio"] = map[string]any{"workload": ";;;;Kubernetes"}
				}
				if fault == "wrong-ip" {
					ip = "10.0.0.2"
				}
				configs = append(configs, map[string]any{"endpoint_config": map[string]any{"cluster_name": "outbound|8080||" + name + "." + ns + ".svc.cluster.local", "endpoints": []any{map[string]any{"lb_endpoints": []any{map[string]any{"endpoint": map[string]any{"address": map[string]any{"socket_address": map[string]any{"address": ip}}}, "metadata": map[string]any{"filter_metadata": meta}}}}}}})
			}
			data, _ := json.Marshal(map[string]any{"configs": configs})
			err := meshValidateEndpoints(data, owners, ns)
			if fault == "" && err != nil {
				t.Fatal(err)
			} else if fault != "" && err == nil {
				t.Fatal("accepted " + fault)
			}
		})
	}
}
