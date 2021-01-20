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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
)

func TestEndpointsToDests(t *testing.T) {
	for _, tc := range []struct {
		name           string
		endpoints      corev1.Endpoints
		protocol       networking.ProtocolType
		expectReady    sets.String
		expectNotReady sets.String
	}{{
		name:        "no endpoints",
		endpoints:   corev1.Endpoints{},
		expectReady: sets.NewString(),
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
		expectReady: sets.NewString("128.0.0.1:1234"),
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
		expectReady: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
	}, {
		name: "single endpoint multiple addresses, including no ready addresses",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}, {
					IP: "128.0.0.2",
				}},
				NotReadyAddresses: []corev1.EndpointAddress{{
					IP: "128.0.0.3",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		},
		expectReady:    sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		expectNotReady: sets.NewString("128.0.0.3:1234"),
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
		expectReady: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "multiple endpoint, different protocol",
		protocol: networking.ProtocolH2C,
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
			}, {
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.3",
				}, {
					IP: "128.0.0.4",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameH2C,
					Port: 5678,
				}},
			}},
		},
		expectReady: sets.NewString("128.0.0.3:5678", "128.0.0.4:5678"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.protocol == "" {
				tc.protocol = networking.ProtocolHTTP1
			}
			ready, notReady := endpointsToDests(&tc.endpoints, networking.ServicePortName(tc.protocol))

			if got, want := ready, tc.expectReady; !got.Equal(want) {
				t.Error("Got unexpected ready dests (-want, +got):", cmp.Diff(want, got))
			}
			if got, want := notReady, tc.expectNotReady; !got.Equal(want) {
				t.Error("Got unexpected notReady dests (-want, +got):", cmp.Diff(want, got))
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

func BenchmarkHealthyAddresses(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000, 10000} {
		b.Run(fmt.Sprint("addresses-", n), func(b *testing.B) {
			ep := eps(10, n)
			for i := 0; i < b.N; i++ {
				healthyAddresses(ep, networking.ServicePortNameHTTP1)
			}
		})
	}
}

func BenchmarkEndpointsToDests(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000, 10000} {
		b.Run(fmt.Sprint("addresses-", n), func(b *testing.B) {
			ep := eps(10, n)
			for i := 0; i < b.N; i++ {
				endpointsToDests(ep, networking.ServicePortNameHTTP1)
			}
		})
	}
}

func eps(activators, apps int) *corev1.Endpoints {
	return &corev1.Endpoints{
		Subsets: []corev1.EndpointSubset{{
			Addresses: addresses("activator", activators),
			Ports: []corev1.EndpointPort{{
				Name: networking.ServicePortNameHTTP1,
				Port: 1234,
			}},
		}, {
			Addresses:         addresses("app", apps),
			NotReadyAddresses: addresses("app-non-ready", apps),
			Ports: []corev1.EndpointPort{{
				Name: networking.ServicePortNameHTTP1,
				Port: 1234,
			}},
		}},
	}
}

func addresses(prefix string, n int) []corev1.EndpointAddress {
	addrs := make([]corev1.EndpointAddress, 0, n)
	for i := 0; i < n; i++ {
		addrs = append(addrs, corev1.EndpointAddress{
			IP: fmt.Sprintf("%s-%d", prefix, i),
		})
	}
	return addrs
}
