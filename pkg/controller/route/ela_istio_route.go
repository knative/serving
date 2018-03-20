/*
Copyright 2017 The Kubernetes Authors.

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
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	istiov1alpha2 "github.com/elafros/elafros/pkg/apis/istio/v1alpha2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeRouteIstioSpec creates an Istio route
func MakeRouteIstioSpec(u *v1alpha1.Route, routes []RevisionRoute) istiov1alpha2.RouteRuleSpec {
	// if either current or next is inactive, target them to proxy instead of
	// the backend so the 0->1 transition will happen.
	placeHolderK8SServiceName := controller.GetElaK8SServiceName(u)
	destinationWeights := []istiov1alpha2.DestinationWeight{}
	for _, route := range routes {
		destinationWeights = append(destinationWeights,
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name: route.Service,
				},
				Weight: route.Weight,
			})
	}
	return istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name: placeHolderK8SServiceName,
		},
		Route: destinationWeights,
	}
}

// MakeRouteIstioRoutes creates an Istio route, owned by the provided v1alpha1.Route.
func MakeRouteIstioRoutes(u *v1alpha1.Route, routes []RevisionRoute) *istiov1alpha2.RouteRule {
	r := &istiov1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetElaIstioRouteRuleName(u),
			Namespace: u.Namespace,
			Labels: map[string]string{
				"route": u.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(u, controllerKind),
			},
		},
		Spec: MakeRouteIstioSpec(u, routes),
	}
	return r
}
