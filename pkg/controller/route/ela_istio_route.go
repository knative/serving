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
	"regexp"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	istiov1alpha2 "github.com/elafros/elafros/pkg/apis/istio/v1alpha2"
	"github.com/elafros/elafros/pkg/controller"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// makeIstioRouteSpec creates an Istio route
func makeIstioRouteSpec(route *v1alpha1.Route, trafficTarget *v1alpha1.TrafficTarget, revRoutes []RevisionRoute, domain string, useActivator bool) istiov1alpha2.RouteRuleSpec {
	destinationWeights := []istiov1alpha2.DestinationWeight{}
	placeHolderK8SServiceName := controller.GetElaK8SServiceName(route)
	// TODO: Set up routerules in different namespace.
	// https://github.com/elafros/elafros/issues/607
	if !useActivator {
		destinationWeights = calculateDestinationWeights(route, trafficTarget, revRoutes)
		if trafficTarget != nil {
			domain = fmt.Sprintf("%s.%s", trafficTarget.Name, domain)
		}
		return istiov1alpha2.RouteRuleSpec{
			Destination: istiov1alpha2.IstioService{
				Name: placeHolderK8SServiceName,
			},
			Match: istiov1alpha2.Match{
				Request: istiov1alpha2.MatchRequest{
					Headers: istiov1alpha2.Headers{
						Authority: istiov1alpha2.MatchString{
							Regex: regexp.QuoteMeta(domain),
						},
					},
				},
			},
			Route: destinationWeights,
		}
	}
	// if enableScaleToZero flag is true, and there are reserved revisions,
	// define the corresponding istio route rules.
	glog.Info("using activator-service as the destination")
	placeHolderK8SServiceName = controller.GetElaK8SActivatorServiceName()
	destinationWeights = append(destinationWeights,
		istiov1alpha2.DestinationWeight{
			Destination: istiov1alpha2.IstioService{
				Name: placeHolderK8SServiceName,
			},
			Weight: 100,
		})
	appendHeaders := make(map[string]string)
	if len(route.Status.Traffic) > 0 {
		// TODO: Deal with the case when the route has more than one traffic targets.
		// Note this has dependency on istio features.
		// https://github.com/elafros/elafros/issues/693
		appendHeaders[controller.GetRevisionHeaderName()] = route.Status.Traffic[0].RevisionName
	}
	return istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name: placeHolderK8SServiceName,
		},
		Route:         destinationWeights,
		AppendHeaders: appendHeaders,
	}
}

// MakeIstioRoutes creates an Istio route
func MakeIstioRoutes(route *v1alpha1.Route, trafficTarget *v1alpha1.TrafficTarget, revRoutes []RevisionRoute, domain string, useActivator bool) *istiov1alpha2.RouteRule {
	labels := map[string]string{"route": route.Name}
	if trafficTarget != nil {
		labels["traffictarget"] = trafficTarget.Name
	}
	r := &istiov1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetRouteRuleName(route, trafficTarget),
			Namespace: route.Namespace,
			Labels:    labels,
		},
		Spec: makeIstioRouteSpec(route, trafficTarget, revRoutes, domain, useActivator),
	}
	serviceRef := controller.NewRouteControllerRef(route)
	r.OwnerReferences = append(r.OwnerReferences, *serviceRef)
	return r
}

// calculateDestinationWeights returns the destination weights for
// the istio route rule.
func calculateDestinationWeights(route *v1alpha1.Route, trafficTarget *v1alpha1.TrafficTarget, revRoutes []RevisionRoute) []istiov1alpha2.DestinationWeight {
	var istioServiceName string

	if trafficTarget != nil {
		for _, revRoute := range revRoutes {
			if revRoute.Name == trafficTarget.Name {
				istioServiceName = revRoute.Service
			}
		}
		return []istiov1alpha2.DestinationWeight{
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name: istioServiceName,
				},
				Weight: 100,
			},
		}
	}

	destinationWeights := []istiov1alpha2.DestinationWeight{}
	for _, revRoute := range revRoutes {
		destinationWeights = append(destinationWeights,
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name: revRoute.Service,
				},
				Weight: revRoute.Weight,
			})
	}
	return destinationWeights
}
