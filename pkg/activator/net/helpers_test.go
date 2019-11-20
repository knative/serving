/*
Copyright 2019 The Knative Authors

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

package net

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/networking"
)

func TestEndpointsToDests(t *testing.T) {
	for _, tc := range []struct {
		name        string
		endpoints   corev1.Endpoints
		protocol    networking.ProtocolType
		expectDests []string
	}{{
		name:        "no endpoints",
		endpoints:   corev1.Endpoints{},
		expectDests: []string{},
	}, {
		name: "single endpoint single address",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		},
		expectDests: []string{"128.0.0.1:1234"},
	}, {
		name: "single endpoint multiple address",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}, {
					IP: "128.0.0.2",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		},
		expectDests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
	}, {
		name: "multiple endpoint filter port",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}, {
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.2",
				}},
				Ports: []corev1.EndpointPort{{
					Name: "other-protocol",
					Port: 1234,
				}},
			}},
		},
		expectDests: []string{"128.0.0.1:1234"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.protocol == "" {
				tc.protocol = networking.ProtocolHTTP1
			}
			dests := endpointsToDests(&tc.endpoints, networking.ServicePortName(tc.protocol))

			if got, want := dests, sets.NewString(tc.expectDests...); !got.Equal(want) {
				t.Errorf("Got unexpected dests (-want, +got): %s", cmp.Diff(want, got))
			}
		})

	}
}

func TestGetServicePort(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol networking.ProtocolType
		ports    []corev1.ServicePort
		expect   int
		expectOK bool
	}{{
		name:     "Single port",
		protocol: networking.ProtocolHTTP1,
		ports: []corev1.ServicePort{{
			Name: "http",
			Port: 100,
		}},
		expect:   100,
		expectOK: true,
	}, {
		name:     "Missing port",
		protocol: networking.ProtocolHTTP1,
		ports: []corev1.ServicePort{{
			Name: "invalid",
			Port: 100,
		}},
		expect:   0,
		expectOK: false,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: tc.ports,
				},
			}

			port, ok := getServicePort(tc.protocol, &svc)
			if ok != tc.expectOK {
				t.Errorf("Wanted ok %v, got %v", tc.expectOK, ok)
			}
			if port != tc.expect {
				t.Errorf("Wanted port %d, got port %d", tc.expect, port)
			}
		})
	}
}
