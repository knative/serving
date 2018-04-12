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
func makeIstioRouteSpec(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget, ns string, routes []RevisionRoute, domain string) istiov1alpha2.RouteRuleSpec {
	destinationWeights := []istiov1alpha2.DestinationWeight{}
	placeHolderK8SServiceName := controller.GetElaK8SServiceName(u)
	// TODO: https://github.com/elafros/elafros/issues/607
	if !enableActivatorExperiment {
		destinationWeights = calculateDestinationWeights(u, tt, routes)
		if tt != nil {
			domain = fmt.Sprintf("%s.%s", tt.Name, domain)
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

	// if enableActivatorExperiment flag is on, and there are reserved revisions,
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
	if len(u.Status.Traffic) > 0 {
		appendHeaders[controller.GetRevisionHeaderName()] = u.Status.Traffic[0].RevisionName
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
func MakeIstioRoutes(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget, ns string, routes []RevisionRoute, domain string) *istiov1alpha2.RouteRule {
	labels := map[string]string{"route": u.Name}
	if tt != nil {
		labels["traffictarget"] = tt.Name
	}

	r := &istiov1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetRouteRuleName(u, tt),
			Namespace: ns,
			Labels:    labels,
		},
		Spec: makeIstioRouteSpec(u, tt, ns, routes, domain),
	}
	serviceRef := metav1.NewControllerRef(u, controllerKind)
	r.OwnerReferences = append(r.OwnerReferences, *serviceRef)
	return r
}

// calculateDestinationWeights returns the destination weights for
// the istio route rule.
func calculateDestinationWeights(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget, routes []RevisionRoute) []istiov1alpha2.DestinationWeight {
	var istioServiceName string

	if tt != nil {
		for _, r := range routes {
			if r.Name == tt.Name {
				istioServiceName = r.Service
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
	for _, route := range routes {
		destinationWeights = append(destinationWeights,
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name: route.Service,
				},
				Weight: route.Weight,
			})
	}
	return destinationWeights
}
