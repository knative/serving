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
	"regexp"
	"testing"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	istiov1alpha2 "github.com/elafros/elafros/pkg/apis/istio/v1alpha2"
	"github.com/elafros/elafros/pkg/controller"
	"github.com/google/go-cmp/cmp"
)

const (
	testDomain   string = "test-domain"
	useActivator bool   = true
)

func getTestRevisionRoutes() []RevisionRoute {
	return []RevisionRoute{
		RevisionRoute{
			Service: "p-deadbeef-service.test", Weight: 98,
		},
		RevisionRoute{
			Service: "test-rev-service.test", Weight: 2,
		},
	}
}

func TestMakeIstioRouteSpecDoNotUseActivator(t *testing.T) {
	route := getTestRouteWithTrafficTargets(nil)
	rr := getTestRevisionRoutes()
	expectedIstioRouteSpec := istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name: "test-route-service",
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
		Route: calculateDestinationWeights(route, nil, rr),
	}

	istioRouteSpec := makeIstioRouteSpec(route, nil, testNamespace, rr, testDomain, !useActivator)

	if diff := cmp.Diff(expectedIstioRouteSpec, istioRouteSpec); diff != "" {
		t.Errorf("Unexpected istio route spec diff (-want +got): %v", diff)
	}
}

func TestMakeIstioRouteSpecUseActivator(t *testing.T) {
	route := getTestRouteWithTrafficTargets(nil)
	rr := getTestRevisionRoutes()
	expectedIstioRouteSpec := istiov1alpha2.RouteRuleSpec{
		Destination: istiov1alpha2.IstioService{
			Name: "test-route-service",
		},
		Route: []istiov1alpha2.DestinationWeight{istiov1alpha2.DestinationWeight{
			Destination: istiov1alpha2.IstioService{
				Name:      controller.GetElaK8SActivatorServiceName(),
				Namespace: controller.GetElaK8SActivatorNamespace(),
			},
			Weight: 100,
		},
		},
		AppendHeaders: map[string]string{},
	}

	istioRouteSpec := makeIstioRouteSpec(route, nil, testNamespace, rr, testDomain, useActivator)

	if diff := cmp.Diff(expectedIstioRouteSpec, istioRouteSpec); diff != "" {
		t.Errorf("Unexpected istio route spec diff (-want +got): %v", diff)
	}
}

func TestCalculateDestinationWeightsNoTrafficTarget(t *testing.T) {
	// The parameter traffic target is nil.
	route := getTestRouteWithTrafficTargets(nil)
	rr := getTestRevisionRoutes()
	expectedDestinationWeights := []istiov1alpha2.DestinationWeight{
		istiov1alpha2.DestinationWeight{
			Destination: istiov1alpha2.IstioService{
				Name: "p-deadbeef-service.test",
			},
			Weight: 98,
		},
		istiov1alpha2.DestinationWeight{
			Destination: istiov1alpha2.IstioService{
				Name: "test-rev-service.test",
			},
			Weight: 2,
		},
	}

	destinationWeights := calculateDestinationWeights(route, nil, rr)

	if diff := cmp.Diff(expectedDestinationWeights, destinationWeights); diff != "" {
		t.Errorf("Unexpected destination weights diff (-want +got): %v", diff)
	}
}

func TestCalculateDestinationWeightsWithTrafficTarget(t *testing.T) {
	tt := v1alpha1.TrafficTarget{
		RevisionName: "test-rev",
		Percent:      100,
	}
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{tt})
	rr := getTestRevisionRoutes()
	expectedDestinationWeights := []istiov1alpha2.DestinationWeight{
		istiov1alpha2.DestinationWeight{
			Destination: istiov1alpha2.IstioService{
				Name: "test-rev-service.test",
			},
			Weight: 100,
		},
	}

	destinationWeights := calculateDestinationWeights(route, &tt, rr)

	if diff := cmp.Diff(expectedDestinationWeights, destinationWeights); diff != "" {
		t.Errorf("Unexpected destination weights diff (-want +got): %v", diff)
	}
}
