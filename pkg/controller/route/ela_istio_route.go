/*
Copyright 2018 Google LLC

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
	"fmt"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	istiov1alpha2 "github.com/elafros/elafros/pkg/apis/istio/v1alpha2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeRouteIstioSpec creates an Istio route
func MakeRouteIstioSpec(u *v1alpha1.Route, ns string, routes []RevisionRoute, domain string) istiov1alpha2.RouteRuleSpec {
	// if either current or next is inactive, target them to proxy instead of
	// the backend so the 0->1 transition will happen.
	placeHolderK8SServiceName := controller.GetElaK8SServiceName(route)
	destinationWeights := []istiov1alpha2.DestinationWeight{}
	for _, revisionRoute := range revisionRoutes {
		destinationWeights = append(destinationWeights,
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name: revisionRoute.Service,
				},
				Weight: revisionRoute.Weight,
			})
	}
	return istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name: placeHolderK8SServiceName,
		},
		Match: istiov1alpha2.Match{
			Request: istiov1alpha2.MatchRequest{
				Headers: istiov1alpha2.Headers{
					Authority: istiov1alpha2.MatchString{
						Regex: domain,
					},
				},
			},
		},
		Route: destinationWeights,
	}
}

// MakeRouteIstioRoutes creates an Istio route
func MakeRouteIstioRoutes(u *v1alpha1.Route, ns string, routes []RevisionRoute, domain string) *istiov1alpha2.RouteRule {
	r := &istiov1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetElaIstioRouteRuleName(route),
			Namespace: route.Namespace,
			Labels: map[string]string{
				"route": route.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(route, controllerKind),
			},
		},
		Spec: MakeRouteIstioSpec(u, ns, routes, domain),
	}
	return r
}

// MakeTrafficTargetIstioRoutes creates Istio route for named traffic targets
func MakeTrafficTargetIstioRoutes(u *v1alpha1.Route, tt v1alpha1.TrafficTarget, ns string, routes []RevisionRoute, domain string) *istiov1alpha2.RouteRule {
	r := &istiov1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetTrafficTargetElaIstioRouteRuleName(u, tt),
			Namespace: ns,
			Labels: map[string]string{
				"route":         u.Name,
				"traffictarget": tt.Name,
			},
		},
		Spec: MakeTrafficTargetRouteIstioSpec(u, tt, ns, routes, domain),
	}
	serviceRef := metav1.NewControllerRef(u, controllerKind)
	r.OwnerReferences = append(r.OwnerReferences, *serviceRef)
	return r
}

// MakeTrafficTargetRouteIstioSpec creates Istio route for named traffic targets
func MakeTrafficTargetRouteIstioSpec(u *v1alpha1.Route, tt v1alpha1.TrafficTarget, ns string, routes []RevisionRoute, domain string) istiov1alpha2.RouteRuleSpec {
	var istioServiceName string

	placeHolderK8SServiceName := controller.GetElaK8SServiceName(u)
	for _, r := range routes {
		if r.Name == tt.Name {
			istioServiceName = r.Service
		}
	}

	return istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name: placeHolderK8SServiceName,
		},
		Match: istiov1alpha2.Match{
			Request: istiov1alpha2.MatchRequest{
				Headers: istiov1alpha2.Headers{
					Authority: istiov1alpha2.MatchString{
						Regex: fmt.Sprintf("%s.%s", tt.Name, domain),
					},
				},
			},
		},
		Route: []istiov1alpha2.DestinationWeight{
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name: istioServiceName,
				},
				Weight: 100,
			},
		},
	}
}
