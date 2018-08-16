/*
Copyright 2018 The Knative Authors

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

package route

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcileVirtualService_Insert(t *testing.T) {
	_, sharedClient, _, c, _, _, _, _ := newTestReconciler(t)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				"route": "test-route",
			},
		},
		Status: v1alpha1.RouteStatus{Domain: "foo.com"},
	}
	vs := newTestVirtualService(r)
	if err := c.reconcileVirtualService(TestContextWithLogger(t), r, vs); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if created, err := sharedClient.NetworkingV1alpha3().VirtualServices(vs.Namespace).Get(vs.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Unexpected error: %v", err)
	} else if diff := cmp.Diff(vs, created); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %v", diff)
	}
}

func TestReconcileVirtualService_Update(t *testing.T) {
	_, sharedClient, _, c, _, sharedInformer, _, _ := newTestReconciler(t)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				"route": "test-route",
			},
		},
	}

	vs := newTestVirtualService(r)
	if err := c.reconcileVirtualService(TestContextWithLogger(t), r, vs); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if updated, err := sharedClient.NetworkingV1alpha3().VirtualServices(vs.Namespace).Get(vs.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Unexpected error: %v", err)
	} else {
		sharedInformer.Networking().V1alpha3().VirtualServices().Informer().GetIndexer().Add(updated)
	}

	r.Status.Domain = "bar.com"
	vs2 := newTestVirtualService(r)
	if err := c.reconcileVirtualService(TestContextWithLogger(t), r, vs2); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if updated, err := sharedClient.NetworkingV1alpha3().VirtualServices(vs.Namespace).Get(vs.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Unexpected error: %v", err)
	} else {
		if diff := cmp.Diff(vs2, updated); diff != "" {
			t.Errorf("Unexpected diff (-want +got): %v", diff)
		}
		if diff := cmp.Diff(vs, updated); diff == "" {
			t.Error("Expected difference, but found none")
		}
	}
}

func newTestVirtualService(r *v1alpha1.Route) *v1alpha3.VirtualService {
	tc := &traffic.TrafficConfig{Targets: map[string][]traffic.RevisionTarget{
		"": {{
			TrafficTarget: v1alpha1.TrafficTarget{
				RevisionName: "revision",
				Percent:      100,
			},
			Active: true,
		}}}}
	return resources.MakeVirtualService(r, tc)
}
