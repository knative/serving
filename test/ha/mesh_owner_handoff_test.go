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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/autoscaler/bucket"
	revnames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

// One service and one continuous traffic generator span all three replacements.
// This is opt-in: it does not alter the ordinary full HA suite.
func TestMeshOwnerHandoffUnderLoad(t *testing.T) {
	if os.Getenv("MESH_HA_LOAD_MODE") != "owner-handoff" {
		t.Skip("Opt-in focused mesh experiment")
	}
	if test.ServingFlags.Buckets != 10 {
		t.Skip("Focused mesh experiment requires ten autoscaler buckets")
	}
	load := e2e.SetupSvc(t, &e2e.AutoscalerOptions{Class: autoscaling.KPA, Metric: autoscaling.Concurrency, Target: 10, TargetUtilization: 0.7}, test.Options{},
		rtesting.WithConfigAnnotations(map[string]string{autoscaling.TargetBurstCapacityKey: "-1"}))
	test.EnsureTearDown(t, load.Clients(), load.Names())
	// Closing done stops the existing traffic helper; no HTTP/retry fix is enabled.
	done := make(chan time.Time)
	trafficWait := e2e.AutoscaleUpToNumPods(load, 1, 8, done, false)
	defer func() {
		close(done)
		if err := trafficWait(); err != nil {
			t.Errorf("Sustained traffic: %v", err)
		}
	}()
	client := load.Clients().KubeClient
	ns := system.Namespace()
	deployment := revnames.Deployment(load.Resources().Revision)
	if err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		d, err := client.AppsV1().Deployments(test.ServingFlags.TestNamespace).Get(ctx, deployment, metav1.GetOptions{})
		return err == nil && d.Status.ReadyReplicas >= 7, nil
	}); err != nil {
		t.Fatalf("Load never reached seven ready replicas: %v", err)
	}
	revisionKey := test.ServingFlags.TestNamespace + "/" + load.Resources().Revision.Name
	for round := 0; round <= 3; round++ {
		var owners map[string]string
		var lastErr error
		err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			owners, lastErr = meshBucketOwners(ctx, client, ns)
			return lastErr == nil, nil
		})
		if err != nil {
			t.Fatalf("Bucket references did not settle: %v (%v)", err, lastErr)
		}
		if dir := os.Getenv("ARTIFACTS"); dir != "" {
			path := filepath.Join(dir, "mesh-focused")
			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(owners)
			if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("round-%d-holders.json", round)), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		// Check every ready autoscaler proxy, not just the owner or load's bucket.
		var pods *corev1.PodList
		err = wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			var err error
			pods, err = client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=autoscaler"})
			if err != nil {
				return false, nil
			}
			ready := 0
			for _, pod := range pods.Items {
				if pod.DeletionTimestamp != nil {
					continue
				}
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						ready++
					}
				}
			}
			return ready == test.ServingFlags.Replicas, nil
		})
		if err != nil {
			t.Fatalf("Autoscaler replicas did not recover: %v", err)
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			var lastErr error
			err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
				commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				data, e := exec.CommandContext(commandCtx, "kubectl", "exec", "-n", ns, pod.Name, "-c", "istio-proxy", "--", "pilot-agent", "request", "GET", "config_dump?include_eds&resource=dynamic_endpoint_configs").Output()
				if e != nil {
					lastErr = e
					return false, nil
				}
				lastErr = meshValidateEndpoints(data, owners, ns)
				if lastErr == nil && os.Getenv("ARTIFACTS") != "" {
					lastErr = os.WriteFile(filepath.Join(os.Getenv("ARTIFACTS"), "mesh-focused", fmt.Sprintf("round-%d-%s-eds.json", round, pod.Name)), data, 0600)
				}
				return lastErr == nil, nil
			})
			if err != nil {
				t.Fatalf("Round %d proxy %s: %v (%v)", round, pod.Name, err, lastErr)
			}
		}
		// Receipt at the owner after the last handoff proves live stat delivery resumed.
		loadBucket := bucket.AutoscalerBucketSet(uint32(test.ServingFlags.Buckets)).Owner(revisionKey)
		holder := owners[loadBucket]
		owner, _, err := bucket.ExtractPodNameAndIP(holder)
		if err != nil {
			t.Fatal(err)
		}
		if err := wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			logs, err := client.CoreV1().Pods(ns).GetLogs(owner, &corev1.PodLogOptions{Container: "autoscaler", SinceSeconds: ptr.To[int64](60), TailLines: ptr.To[int64](2000)}).DoRaw(ctx)
			if err != nil {
				return false, nil
			}
			for _, line := range strings.Split(string(logs), "\n") {
				var event map[string]any
				if json.Unmarshal([]byte(line), &event) == nil && event["knative.dev/key"] == revisionKey && event["message"] == "Accept stat as owner of bucket "+loadBucket {
					return true, nil
				}
			}
			return false, nil
		}); err != nil {
			t.Fatalf("New owner %s did not accept live stats: %v", owner, err)
		}
		t.Logf("Round %d: all ten references and proxy TLS views correct; %s accepts %s", round, owner, revisionKey)
		if round == 3 {
			break
		}
		// Delete all current holders (usually one), leaving non-owners to take over.
		unique := map[string]bool{}
		for _, holder := range owners {
			owner, _, err := bucket.ExtractPodNameAndIP(holder)
			if err != nil {
				t.Fatal(err)
			}
			if unique[owner] {
				continue
			}
			unique[owner] = true
			t.Logf("Round %d: replacing owner %s while traffic continues", round+1, owner)
			if err := client.CoreV1().Pods(ns).Delete(t.Context(), owner, metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		// Do not accept old slices/owners as proof of a completed handoff.
		if err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			next, err := meshBucketOwners(ctx, client, ns)
			if err != nil {
				return false, nil
			}
			for _, holder := range next {
				name, _, _ := bucket.ExtractPodNameAndIP(holder)
				if unique[name] {
					return false, nil
				}
			}
			return true, nil
		}); err != nil {
			t.Fatalf("Owner replacement did not complete: %v", err)
		}
	}
}

// Kubernetes and Istio have separate caches: require both to agree with leases.
func meshBucketOwners(ctx context.Context, client kubernetes.Interface, ns string) (map[string]string, error) {
	leases, err := client.CoordinationV1().Leases(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	owners := map[string]string{}
	for _, lease := range leases.Items {
		if !strings.HasPrefix(lease.Name, "autoscaler-bucket-") {
			continue
		}
		holder := ptr.Deref(lease.Spec.HolderIdentity, "")
		podName, ip, err := bucket.ExtractPodNameAndIP(holder)
		if err != nil || podName == "" {
			return nil, fmt.Errorf("invalid holder for %s: %q", lease.Name, holder)
		}
		pod, err := client.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if pod.DeletionTimestamp != nil || pod.Status.PodIP != ip {
			return nil, fmt.Errorf("stale holder %s", holder)
		}
		slice, err := client.DiscoveryV1().EndpointSlices(ns).Get(ctx, lease.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if len(slice.Endpoints) != 1 {
			return nil, fmt.Errorf("unexpected endpoints for %s", lease.Name)
		}
		endpoint := slice.Endpoints[0]
		ref := endpoint.TargetRef
		if len(endpoint.Addresses) != 1 || endpoint.Addresses[0] != ip || ref == nil || ref.APIVersion != "v1" || ref.Kind != "Pod" || ref.Namespace != ns || ref.Name != podName {
			return nil, fmt.Errorf("slice %s does not reference holder %s", lease.Name, holder)
		}
		owners[lease.Name] = holder
	}
	if len(owners) != 10 {
		return nil, fmt.Errorf("got %d buckets, want 10", len(owners))
	}
	return owners, nil
}

func meshValidateEndpoints(data []byte, owners map[string]string, ns string) error {
	var dump struct {
		Configs []struct {
			EndpointConfig struct {
				ClusterName string `json:"cluster_name"`
				Endpoints   []struct {
					LBEndpoints []struct {
						Endpoint struct {
							Address struct {
								SocketAddress struct{ Address string } `json:"socket_address"`
							}
						}
						Metadata struct {
							FilterMetadata map[string]map[string]any `json:"filter_metadata"`
						}
					} `json:"lb_endpoints"`
				}
			} `json:"endpoint_config"`
		}
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, config := range dump.Configs {
		ep := config.EndpointConfig
		for name, holder := range owners {
			if ep.ClusterName != "outbound|8080||"+name+"."+ns+".svc.cluster.local" {
				continue
			}
			_, ip, err := bucket.ExtractPodNameAndIP(holder)
			if err != nil {
				return err
			}
			for _, locality := range ep.Endpoints {
				for _, host := range locality.LBEndpoints {
					tls := host.Metadata.FilterMetadata["envoy.transport_socket_match"]["tlsMode"]
					workload, _ := host.Metadata.FilterMetadata["istio"]["workload"].(string)
					if host.Endpoint.Address.SocketAddress.Address != ip || tls != "istio" || !strings.HasPrefix(workload, "autoscaler;"+ns+";") {
						return fmt.Errorf("%s: wrong IP/TLS/workload: %s %v %s", name, host.Endpoint.Address.SocketAddress.Address, tls, workload)
					}
					seen[name] = true
				}
			}
		}
	}
	if len(seen) != len(owners) {
		return fmt.Errorf("got %d/%d bucket endpoints", len(seen), len(owners))
	}
	return nil
}
