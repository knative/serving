/*
Copyright 2019 The Knative Authors.

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

package resources

import (
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	apiconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
)

var (
	defaultMaxRevisionTimeout = time.Duration(apiconfig.DefaultMaxRevisionTimeoutSeconds) * time.Second
	defaultGateways           = makeGatewayMap([]string{"gateway"}, []string{"private-gateway"})
	defaultIngress            = v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
		},
		Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test-ns.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{},
		}}},
	}
)

func TestMakeVirtualServices_CorrectMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gateways map[v1alpha1.IngressVisibility]sets.String
		ci       *v1alpha1.Ingress
		expected []metav1.ObjectMeta
	}{{
		name:     "mesh and ingress",
		gateways: makeGatewayMap([]string{"gateway"}, []string{"private-gateway"}),
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					networking.IngressLabelKey:     "test-ingress",
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}, {
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}, {
		name:     "ingress only",
		gateways: makeGatewayMap([]string{"gateway"}, []string{"private-gateway"}),
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.example.com",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}, {
		name:     "mesh only",
		gateways: nil,
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}, {
		name:     "mesh only with namespace",
		gateways: nil,
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: "test-ns",
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: "test-ns",
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			vss, err := MakeVirtualServices(tc.ci, tc.gateways)
			if err != nil {
				t.Fatalf("MakeVirtualServices failed: %v", err)
			}
			if len(vss) != len(tc.expected) {
				t.Fatalf("Expected %d VirtualService, saw %d", len(tc.expected), len(vss))
			}
			for i := range tc.expected {
				tc.expected[i].OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(tc.ci)}
				if diff := cmp.Diff(tc.expected[i], vss[i].ObjectMeta); diff != "" {
					t.Errorf("Unexpected metadata (-want +got): %v", diff)
				}
			}
		})
	}
}

func TestMakeMeshVirtualServiceSpec_CorrectGateways(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
	}
	expected := []string{"mesh"}
	gateways := MakeMeshVirtualService(ci, defaultGateways).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Errorf("Unexpected gateways (-want +got): %v", diff)
	}
}

func TestMakeMeshVirtualServiceSpecCorrectHosts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		gateways      map[v1alpha1.IngressVisibility]sets.String
		expectedHosts sets.String
	}{{
		name: "with cluster local gateway: expanding hosts",
		gateways: map[v1alpha1.IngressVisibility]sets.String{
			v1alpha1.IngressVisibilityClusterLocal: sets.NewString("cluster-local"),
		},
		expectedHosts: sets.NewString(
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
		),
	}, {
		name:          "with mesh: no exapnding hosts",
		gateways:      map[v1alpha1.IngressVisibility]sets.String{},
		expectedHosts: sets.NewString("test-route.test-ns.svc.cluster.local"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			vs := MakeMeshVirtualService(&defaultIngress, tc.gateways)
			vsHosts := sets.NewString(vs.Spec.Hosts...)
			if !vsHosts.Equal(tc.expectedHosts) {
				t.Errorf("Unexpected hosts want %v; got %v", tc.expectedHosts, vsHosts)
			}
		})
	}

}

func TestMakeMeshVirtualServiceSpec_CorrectRetries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ci       *v1alpha1.Ingress
		expected *istiov1alpha3.HTTPRetry
	}{{
		name: "default retries",
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"test-route.test-ns.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Retries: &v1alpha1.HTTPRetry{
								PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
								Attempts:      networking.DefaultRetryCount,
							},
						}},
					},
				}}},
		},
		expected: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}, {
		name: "disabling retries",
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"test-route.test-ns.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Retries: &v1alpha1.HTTPRetry{
								PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
								Attempts:      0,
							},
						}},
					},
				}}},
		},
		expected: &istiov1alpha3.HTTPRetry{
			RetryOn:       "",
			Attempts:      int32(0),
			PerTryTimeout: nil,
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, h := range MakeMeshVirtualService(tc.ci, defaultGateways).Spec.Http {
				if diff := cmp.Diff(tc.expected, h.Retries); diff != "" {
					t.Errorf("Unexpected retries (-want +got): %v", diff)
				}
			}
		})
	}
}

func TestMakeMeshVirtualServiceSpec_CorrectRoutes(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v2-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"ugh": "blah",
							},
						}},
						AppendHeaders: map[string]string{
							"foo": "bar",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v1-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
						}},
						AppendHeaders: map[string]string{
							"foo": "baz",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
			}},
		},
	}
	expected := []*istiov1alpha3.HTTPRoute{{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `test-route.test-ns`},
			},
			Gateways: []string{"mesh"},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &istiov1alpha3.Headers{
				Request: &istiov1alpha3.Headers_HeaderOperations{
					Set: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "bar",
				},
			},
		},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
		WebsocketUpgrade: true,
	}}

	routes := MakeMeshVirtualService(ci, defaultGateways).Spec.Http
	if diff := cmp.Diff(expected, routes); diff != "" {
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

func TestMakeIngressVirtualServiceSpec_CorrectGateways(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		},
		Spec: v1alpha1.IngressSpec{},
	}
	expected := []string{"knative-testing/gateway-one", "knative-testing/gateway-two"}
	gateways := MakeIngressVirtualService(ci, makeGatewayMap([]string{"knative-testing/gateway-one", "knative-testing/gateway-two"}, nil)).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Errorf("Unexpected gateways (-want +got): %v", diff)
	}
}

func TestMakeIngressVirtualServiceSpec_CorrectRoutes(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"domain.com",
					"test-route.test-ns.svc.cluster.local",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v2-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"ugh": "blah",
							},
						}},
						AppendHeaders: map[string]string{
							"foo": "bar",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v1-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
						}},
						AppendHeaders: map[string]string{
							"foo": "baz",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
			}},
		},
	}

	expected := []*istiov1alpha3.HTTPRoute{{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `domain.com`},
			},
			Gateways: []string{"gateway.public"},
		}, {
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `test-route.test-ns`},
			},
			Gateways: []string{"gateway.private"},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &istiov1alpha3.Headers{
				Request: &istiov1alpha3.Headers_HeaderOperations{
					Set: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "bar",
				},
			},
		},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
		WebsocketUpgrade: true,
	}, {
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `v1.domain.com`},
			},
			Gateways: []string{},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "v1-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Headers: &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "baz",
				},
			},
		},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
		WebsocketUpgrade: true,
	}}

	routes := MakeIngressVirtualService(ci, makeGatewayMap([]string{"gateway.public"}, []string{"gateway.private"})).Spec.Http
	if diff := cmp.Diff(expected, routes); diff != "" {
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

// One active target.
func TestMakeVirtualServiceRoute_Vanilla(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		Splits: []v1alpha1.IngressBackendSplit{{
			IngressBackend: v1alpha1.IngressBackend{

				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 100,
		}},
		Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
			Attempts:      networking.DefaultRetryCount,
		},
	}
	route := makeVirtualServiceRoute(sets.NewString("a.com", "b.org"), ingressPath, makeGatewayMap([]string{"gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1alpha3.HTTPRoute{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Gateways: []string{"gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `a.com`},
			},
		}, {
			Gateways: []string{"gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `b.org`},
			},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
		WebsocketUpgrade: true,
	}
	if diff := cmp.Diff(expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// Two active targets.
func TestMakeVirtualServiceRoute_TwoTargets(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		Splits: []v1alpha1.IngressBackendSplit{{
			IngressBackend: v1alpha1.IngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 90,
		}, {
			IngressBackend: v1alpha1.IngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "new-revision-service",
				ServicePort:      intstr.FromInt(81),
			},
			Percent: 10,
		}},
		Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
			Attempts:      networking.DefaultRetryCount,
		},
	}
	route := makeVirtualServiceRoute(sets.NewString("test.org"), ingressPath, makeGatewayMap([]string{"knative-testing/gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1alpha3.HTTPRoute{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Gateways: []string{"knative-testing/gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `test.org`},
			},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 90,
		}, {
			Destination: &istiov1alpha3.Destination{
				Host: "new-revision-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 81},
			},
			Weight: 10,
		}},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
		WebsocketUpgrade: true,
	}
	if diff := cmp.Diff(expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

func TestGetHosts_Duplicate(t *testing.T) {
	ci := &v1alpha1.Ingress{
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{"test-route1", "test-route2"},
			}, {
				Hosts: []string{"test-route1", "test-route3"},
			}},
		},
	}
	hosts := getHosts(ci)
	expected := sets.NewString("test-route1", "test-route2", "test-route3")
	if diff := cmp.Diff(expected, hosts); diff != "" {
		t.Errorf("Unexpected hosts  (-want +got): %v", diff)
	}
}

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.String {
	return map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP:   sets.NewString(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.NewString(privateGateways...),
	}
}
