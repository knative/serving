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
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	istiov1alpha2 "github.com/knative/serving/pkg/apis/istio/v1alpha2"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
)

const (
	testDomain       = "test-domain"
	emptyInactiveRev = ""
	testInactiveRev  = "test-rev"
)

func getTestRevisionRoutes() []RevisionRoute {
	return []RevisionRoute{{
		Service:   "p-deadbeef-service",
		Namespace: testNamespace,
		Weight:    98,
	}, {
		Service:   "test-rev-service",
		Namespace: testNamespace,
		Weight:    2,
	}}
}

func TestMakeIstioRouteSpecRevisionsActive(t *testing.T) {
	route := getTestRouteWithTrafficTargets(nil)
	rr := getTestRevisionRoutes()
	expectedIstioRouteSpec := istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: istiov1alpha2.Match{
			Request: istiov1alpha2.MatchRequest{
				Headers: istiov1alpha2.Headers{
					Authority: istiov1alpha2.MatchString{
						Regex: regexp.QuoteMeta(testDomain),
					},
				},
			},
		},
		Route: calculateDestinationWeights(nil, rr),
	}

	istioRouteSpec := makeIstioRouteSpec(route, nil, testNamespace, rr, testDomain, emptyInactiveRev)

	if diff := cmp.Diff(expectedIstioRouteSpec, istioRouteSpec); diff != "" {
		t.Errorf("Unexpected istio route spec diff (-want +got): %v", diff)
	}
}

func TestMakeIstioRouteSpecRevisionInactive(t *testing.T) {
	route := getTestRouteWithTrafficTargets(nil)
	rr := getTestRevisionRoutes()
	appendHeaders := make(map[string]string)
	appendHeaders[controller.GetRevisionHeaderName()] = testInactiveRev
	appendHeaders[controller.GetRevisionHeaderNamespace()] = testNamespace
	appendHeaders["x-envoy-upstream-rq-timeout-ms"] = requestTimeoutMs
	expectedIstioRouteSpec := istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: istiov1alpha2.Match{
			Request: istiov1alpha2.MatchRequest{
				Headers: istiov1alpha2.Headers{
					Authority: istiov1alpha2.MatchString{
						Regex: regexp.QuoteMeta(testDomain),
					},
				},
			},
		},
		Route: []istiov1alpha2.DestinationWeight{{
			Destination: istiov1alpha2.IstioService{
				Name:      "p-deadbeef-service",
				Namespace: testNamespace,
			},
			Weight: 98,
		}, {
			Destination: istiov1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 2,
		}},
		AppendHeaders: appendHeaders,
	}

	istioRouteSpec := makeIstioRouteSpec(route, nil, testNamespace, rr, testDomain, testInactiveRev)

	if diff := cmp.Diff(expectedIstioRouteSpec, istioRouteSpec); diff != "" {
		t.Errorf("Unexpected istio route spec diff (-want +got): %v", diff)
	}
}

func TestCalculateDestinationWeightsNoTrafficTarget(t *testing.T) {
	rr := getTestRevisionRoutes()
	expectedDestinationWeights := []istiov1alpha2.DestinationWeight{{
		Destination: istiov1alpha2.IstioService{
			Name:      "p-deadbeef-service",
			Namespace: testNamespace,
		},
		Weight: 98,
	}, {
		Destination: istiov1alpha2.IstioService{
			Name:      "test-rev-service",
			Namespace: testNamespace,
		},
		Weight: 2,
	}}

	destinationWeights := calculateDestinationWeights(nil, rr)

	if diff := cmp.Diff(expectedDestinationWeights, destinationWeights); diff != "" {
		t.Errorf("Unexpected destination weights diff (-want +got): %v", diff)
	}
}

func TestCalculateDestinationWeightsWithTrafficTarget(t *testing.T) {
	tt := v1alpha1.TrafficTarget{
		RevisionName: "test-rev",
		Percent:      100,
	}
	rr := getTestRevisionRoutes()
	expectedDestinationWeights := []istiov1alpha2.DestinationWeight{{
		Destination: istiov1alpha2.IstioService{
			Name:      "test-rev-service",
			Namespace: testNamespace,
		},
		Weight: 100,
	}}

	destinationWeights := calculateDestinationWeights(&tt, rr)

	if diff := cmp.Diff(expectedDestinationWeights, destinationWeights); diff != "" {
		t.Errorf("Unexpected destination weights diff (-want +got): %v", diff)
	}
}
