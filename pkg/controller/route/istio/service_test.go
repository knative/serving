/*
Copyright 2018 The Knative Authors.

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

package istio

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeRouteK8SService_ValidSpec(t *testing.T) {
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				"route": "test-route",
			},
		},
	}
	expectedSpec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{{
			Name: "http",
			Port: 80,
		}},
		ClusterIP: "None",
	}
	spec := MakeRouteK8SService(r).Spec
	if diff := cmp.Diff(expectedSpec, spec); diff != "" {
		t.Errorf("Unexpected ServiceSpec (-want +got): %v", diff)
	}
}

func TestMakeRouteK8SService_ValidMeta(t *testing.T) {
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				"route": "test-route",
			},
		},
	}
	expectedMeta := metav1.ObjectMeta{
		Name:      "test-route-service",
		Namespace: "test-ns",
		OwnerReferences: []metav1.OwnerReference{
			// This service is owned by the Route.
			*controller.NewRouteControllerRef(r),
		},
	}
	meta := MakeRouteK8SService(r).ObjectMeta
	if diff := cmp.Diff(expectedMeta, meta); diff != "" {
		t.Errorf("Unexpected Metadata (-want +got): %v", diff)
	}
}
