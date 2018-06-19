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

	istiov1alpha2 "github.com/knative/serving/pkg/apis/istio/v1alpha2"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const requestTimeoutMs = "60000"

// makeIstioRouteSpec creates an Istio route
func makeIstioRouteSpec(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget, ns string, routes []RevisionRoute, domain string, inactiveRev string) istiov1alpha2.RouteRuleSpec {
	destinationWeights := calculateDestinationWeights(u, tt, routes)
	if tt != nil {
		domain = fmt.Sprintf("%s.%s", tt.Name, domain)
	}
	spec := istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name:      controller.GetServingK8SServiceName(u),
			Namespace: ns,
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

	// TODO: The ideal solution is to append different revision name as headers for each inactive revision.
	// See https://github.com/istio/issues/issues/332
	// Since appendHeaders is a field for RouteRule Spec, we don't have that granularity.
	// We will direct traffic for all inactive revisions to activator service; and the activator will send
	// the request to the inactive revision with the largest traffic weight.
	// The consequence of using appendHeaders at Spec is: if there are more than one inactive revisions, the
	// traffic split percentage would be distorted in a short period of time.
	if inactiveRev != "" {
		appendHeaders := make(map[string]string)
		appendHeaders[controller.GetRevisionHeaderName()] = inactiveRev
		appendHeaders[controller.GetRevisionHeaderNamespace()] = u.Namespace
		// Set the Envoy upstream timeout to be 60 seconds, in case the revision needs longer time to come up
		// https://www.envoyproxy.io/docs/envoy/v1.5.0/configuration/http_filters/router_filter#x-envoy-upstream-rq-timeout-ms
		appendHeaders["x-envoy-upstream-rq-timeout-ms"] = requestTimeoutMs
		spec.AppendHeaders = appendHeaders
	}
	return spec
}

// MakeIstioRoutes creates an Istio route
func MakeIstioRoutes(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget, ns string, routes []RevisionRoute, domain string, inactiveRev string) *istiov1alpha2.RouteRule {
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
		Spec: makeIstioRouteSpec(u, tt, ns, routes, domain, inactiveRev),
	}
	serviceRef := controller.NewRouteControllerRef(u)
	r.OwnerReferences = append(r.OwnerReferences, *serviceRef)
	return r
}

// calculateDestinationWeights returns the destination weights for
// the istio route rule.
func calculateDestinationWeights(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget, routes []RevisionRoute) []istiov1alpha2.DestinationWeight {
	var istioServiceName string
	var istioServiceNs string

	if tt != nil {
		for _, r := range routes {
			if r.Name == tt.Name {
				istioServiceName = r.Service
				istioServiceNs = r.Namespace
			}
		}
		return []istiov1alpha2.DestinationWeight{
			istiov1alpha2.DestinationWeight{
				Destination: istiov1alpha2.IstioService{
					Name:      istioServiceName,
					Namespace: istioServiceNs,
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
					Name:      route.Service,
					Namespace: route.Namespace,
				},
				Weight: route.Weight,
			})
	}
	return destinationWeights
}
