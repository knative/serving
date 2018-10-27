/*
Copyright 2018 The Knative Authors.

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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	istiov1alpha1 "github.com/knative/pkg/apis/istio/common/v1alpha1"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/system"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestMakeVirtualServiceSpec_CorrectMetadata(t *testing.T) {
	ci := &v1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ingress",
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		},
		Spec: v1alpha1.IngressSpec{},
	}
	expected := metav1.ObjectMeta{
		Name:      "test-ingress",
		Namespace: system.Namespace,
		Labels: map[string]string{
			networking.IngressLabelKey:     "test-ingress",
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: "test-ns",
		},
		OwnerReferences: []metav1.OwnerReference{
			*kmeta.NewControllerRef(ci),
		},
	}
	meta := MakeVirtualService(ci).ObjectMeta
	if diff := cmp.Diff(expected, meta); diff != "" {
		t.Errorf("Unexpected metadata (-want +got): %v", diff)
	}
}

func TestMakeVirtualServiceSpec_CorrectRoutes(t *testing.T) {
	ci := &v1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ingress",
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.ClusterIngressRule{{
				Hosts: []string{
					"domain.com",
					"test-route.test-ns.svc.cluster.local",
					"test-route.test-ns.svc",
					"test-route.test-ns",
					"test-route",
				},
				HTTP: &v1alpha1.HTTPClusterIngressRuleValue{
					Paths: []v1alpha1.HTTPClusterIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.ClusterIngressBackendSplit{{
							ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v2-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
						}},
						Timeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
							Attempts:      v1alpha1.DefaultRetryCount,
						},
					}},
				},
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				HTTP: &v1alpha1.HTTPClusterIngressRuleValue{
					Paths: []v1alpha1.HTTPClusterIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.ClusterIngressBackendSplit{{
							ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v1-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
						}},
						Timeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
							Attempts:      v1alpha1.DefaultRetryCount,
						},
					}},
				},
			},
			},
		},
	}
	expected := []v1alpha3.HTTPRoute{{
		Match: []v1alpha3.HTTPMatchRequest{{
			Uri:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Exact: "domain.com"},
		}, {
			Uri:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route.test-ns.svc.cluster.local"},
		}, {
			Uri:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route.test-ns.svc"},
		}, {
			Uri:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route.test-ns"},
		}, {
			Uri:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: v1alpha1.DefaultTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      v1alpha1.DefaultRetryCount,
			PerTryTimeout: v1alpha1.DefaultTimeout.String(),
		},
	}, {
		Match: []v1alpha3.HTTPMatchRequest{{
			Uri:       &istiov1alpha1.StringMatch{Regex: "^/pets/(.*?)?"},
			Authority: &istiov1alpha1.StringMatch{Exact: "v1.domain.com"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "v1-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: v1alpha1.DefaultTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      v1alpha1.DefaultRetryCount,
			PerTryTimeout: v1alpha1.DefaultTimeout.String(),
		},
	}}
	routes := MakeVirtualService(ci).Spec.Http
	if diff := cmp.Diff(expected, routes); diff != "" {
		fmt.Printf("%+v\n", routes)
		fmt.Printf("%+v\n", expected)
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

// One active target.
func TestMakeVirtualServiceRoute_Vanilla(t *testing.T) {
	ingressPath := &v1alpha1.HTTPClusterIngressPath{
		Splits: []v1alpha1.ClusterIngressBackendSplit{{
			ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 100,
		}},
		Timeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
			Attempts:      v1alpha1.DefaultRetryCount,
		},
	}
	hosts := []string{"a.com", "b.org"}
	route := makeVirtualServiceRoute(hosts, ingressPath)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "a.com"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "b.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: v1alpha1.DefaultTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      v1alpha1.DefaultRetryCount,
			PerTryTimeout: v1alpha1.DefaultTimeout.String(),
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// Two active targets.
func TestMakeVirtualServiceRoute_TwoTargets(t *testing.T) {
	ingressPath := &v1alpha1.HTTPClusterIngressPath{
		Splits: []v1alpha1.ClusterIngressBackendSplit{{
			ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 90,
		}, {
			ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "new-revision-service",
				ServicePort:      intstr.FromString("test-port"),
			},
			Percent: 10,
		}},
		Timeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
			Attempts:      v1alpha1.DefaultRetryCount,
		},
	}
	hosts := []string{"test.org"}
	route := makeVirtualServiceRoute(hosts, ingressPath)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "test.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
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
		Timeout: v1alpha1.DefaultTimeout.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      v1alpha1.DefaultRetryCount,
			PerTryTimeout: v1alpha1.DefaultTimeout.String(),
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

func TestGetHosts_Duplicate(t *testing.T) {
	ci := &v1alpha1.ClusterIngress{
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.ClusterIngressRule{{
				Hosts: []string{
					"test-route1",
					"test-route2",
				},
			}, {
				Hosts: []string{
					"test-route1",
					"test-route3",
				},
			}},
		},
	}
	hosts := getHosts(ci)
	expected := []string{
		"test-route1",
		"test-route2",
		"test-route3",
	}
	if diff := cmp.Diff(expected, hosts); diff != "" {
		t.Errorf("Unexpected hosts  (-want +got): %v", diff)
	}
}
