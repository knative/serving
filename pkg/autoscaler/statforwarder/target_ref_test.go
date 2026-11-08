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

package statforwarder

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	discoverylisters "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestBucketEndpointPodReference(t *testing.T) {
	const namespace, name, ip = "serving-system", "autoscaler-bucket-00-of-10", "10.0.0.1"
	for _, tc := range []struct {
		name     string
		existing bool
		ref      *corev1.ObjectReference
	}{
		{name: "create"},
		{name: "upgrade_missing_reference", existing: true},
		{name: "replace_stale_reference_same_ip", existing: true, ref: &corev1.ObjectReference{Kind: "Pod", Namespace: namespace, Name: "old-holder"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := kubefake.NewSimpleClientset()
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			if tc.existing {
				slice := &discoveryv1.EndpointSlice{
					ObjectMeta:  metav1.ObjectMeta{Name: name, Namespace: namespace},
					AddressType: discoveryv1.AddressTypeIPv4,
					Endpoints: []discoveryv1.Endpoint{{Addresses: []string{ip}, TargetRef: tc.ref,
						Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true), Serving: ptr.To(true), Terminating: ptr.To(false)}}},
				}
				if _, err := client.DiscoveryV1().EndpointSlices(namespace).Create(t.Context(), slice, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := indexer.Add(slice); err != nil {
					t.Fatal(err)
				}
			}
			tracker := &leaseTracker{logger: logtesting.TestLogger(t), selfIP: ip, kc: client,
				endpointsLister: discoverylisters.NewEndpointSliceLister(indexer)}
			if err := tracker.createOrUpdateEndpoints(context.Background(), namespace, name, "new-holder_"+ip); err != nil {
				t.Fatal(err)
			}
			got, err := client.DiscoveryV1().EndpointSlices(namespace).Get(t.Context(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			want := &corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Namespace: namespace, Name: "new-holder"}
			if diff := cmp.Diff(want, got.Endpoints[0].TargetRef); diff != "" {
				t.Errorf("Pod reference (-want +got): %s", diff)
			}
			if got.Endpoints[0].Addresses[0] != ip {
				t.Error("adding the Pod reference changed the endpoint IP")
			}
		})
	}
}
