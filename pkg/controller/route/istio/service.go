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

package istio

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeRouteK8SService creates a Service that targets nothing, owned by the provided v1alpha1.Route.  The purpose of
// this service is to provide a FQDN for Istio routing.  Since Istio does not provide IP address routing, a ClusterIP is
// useless here.  As a result we assign ClusterIP: "None" for this Service to avoid redundant IP assignment.
func MakeRouteK8SService(route *v1alpha1.Route) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetServingK8SServiceNameForRoute(route),
			Namespace: route.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				// This service is owned by the Route.
				*controller.NewRouteControllerRef(route),
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: PortName,
					Port: PortNumber,
				},
			},
		},
	}
}
