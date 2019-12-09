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

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	istiov1alpha1 "knative.dev/pkg/apis/istio/common/v1alpha1"
	"knative.dev/pkg/apis/istio/v1alpha3"
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
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}, {
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
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
			Spec: v1alpha1.IngressSpec{},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: system.Namespace(),
			Labels: map[string]string{
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
			Spec: v1alpha1.IngressSpec{},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: "test-ns",
			Labels: map[string]string{
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
		Spec: v1alpha1.IngressSpec{},
	}
	expected := []string{"mesh"}
	gateways := MakeMeshVirtualService(ci).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Errorf("Unexpected gateways (-want +got): %v", diff)
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
					"test-route.test-ns.svc",
					"test-route.test-ns",
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
	expected := []v1alpha3.HTTPRoute{{
		Match: []v1alpha3.HTTPMatchRequest{{
			URI:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `test-route.test-ns`},
			Gateways:  []string{"mesh"},
		}},
		Route: []v1alpha3.HTTPRouteDestination{{
			Destination: v1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &v1alpha3.Headers{
				Request: &v1alpha3.HeaderOperations{
					Add: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &v1alpha3.Headers{
			Request: &v1alpha3.HeaderOperations{
				Add: map[string]string{
					"foo": "bar",
				},
			},
		},
		Timeout: defaultMaxRevisionTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      networking.DefaultRetryCount,
			PerTryTimeout: defaultMaxRevisionTimeout.String(),
		},
		WebsocketUpgrade: true,
	}}

	routes := MakeMeshVirtualService(ci).Spec.HTTP
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

	expected := []v1alpha3.HTTPRoute{{
		Match: []v1alpha3.HTTPMatchRequest{{
			URI:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `domain.com`},
			Gateways:  []string{"gateway.public"},
		}, {
			URI:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `test-route.test-ns`},
			Gateways:  []string{"gateway.private"},
		}},
		Route: []v1alpha3.HTTPRouteDestination{{
			Destination: v1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &v1alpha3.Headers{
				Request: &v1alpha3.HeaderOperations{
					Add: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &v1alpha3.Headers{
			Request: &v1alpha3.HeaderOperations{
				Add: map[string]string{
					"foo": "bar",
				},
			},
		},
		Timeout: defaultMaxRevisionTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      networking.DefaultRetryCount,
			PerTryTimeout: defaultMaxRevisionTimeout.String(),
		},
		WebsocketUpgrade: true,
	}, {
		Match: []v1alpha3.HTTPMatchRequest{{
			URI:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `v1.domain.com`},
			Gateways:  []string{},
		}},
		Route: []v1alpha3.HTTPRouteDestination{{
			Destination: v1alpha3.Destination{
				Host: "v1-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Headers: &v1alpha3.Headers{
			Request: &v1alpha3.HeaderOperations{
				Add: map[string]string{
					"foo": "baz",
				},
			},
		},
		Timeout: defaultMaxRevisionTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      networking.DefaultRetryCount,
			PerTryTimeout: defaultMaxRevisionTimeout.String(),
		},
		WebsocketUpgrade: true,
	}}

	routes := MakeIngressVirtualService(ci, makeGatewayMap([]string{"gateway.public"}, []string{"gateway.private"})).Spec.HTTP
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
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Gateways:  []string{"gateway-1"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `a.com`},
		}, {
			Gateways:  []string{"gateway-1"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `b.org`},
		}},
		Route: []v1alpha3.HTTPRouteDestination{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: defaultMaxRevisionTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      networking.DefaultRetryCount,
			PerTryTimeout: defaultMaxRevisionTimeout.String(),
		},
		WebsocketUpgrade: true,
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
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
				ServicePort:      intstr.FromString("test-port"),
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
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Gateways:  []string{"knative-testing/gateway-1"},
			Authority: &istiov1alpha1.StringMatch{Prefix: `test.org`},
		}},
		Route: []v1alpha3.HTTPRouteDestination{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 90,
		}, {
			Destination: v1alpha3.Destination{
				Host: "new-revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Name: "test-port"},
			},
			Weight: 10,
		}},
		Timeout: defaultMaxRevisionTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      networking.DefaultRetryCount,
			PerTryTimeout: defaultMaxRevisionTimeout.String(),
		},
		WebsocketUpgrade: true,
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
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

func TestGetExpandedHosts(t *testing.T) {
	for _, test := range []struct {
		name  string
		hosts sets.String
		want  sets.String
	}{{
		name: "cluster local service in non-default namespace",
		hosts: sets.NewString(
			"service.namespace.svc.cluster.local",
		),
		want: sets.NewString(
			"service.namespace",
			"service.namespace.svc",
			"service.namespace.svc.cluster.local",
		),
	}, {
		name: "example.com service",
		hosts: sets.NewString(
			"foo.bar.example.com",
		),
		want: sets.NewString(
			"foo.bar.example.com",
		),
	}, {
		name: "default.example.com service",
		hosts: sets.NewString(
			"foo.default.example.com",
		),
		want: sets.NewString(
			"foo.default.example.com",
		),
	}, {
		name: "mix",
		hosts: sets.NewString(
			"foo.default.example.com",
			"foo.default.svc.cluster.local",
		),
		want: sets.NewString(
			"foo.default",
			"foo.default.example.com",
			"foo.default.svc",
			"foo.default.svc.cluster.local",
		),
	}} {
		t.Run(test.name, func(t *testing.T) {
			got := expandedHosts(test.hosts)
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("Unexpected (-want +got): %v", diff)
			}
		})
	}
}

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.String {
	return map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP:   sets.NewString(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.NewString(privateGateways...),
	}
}
