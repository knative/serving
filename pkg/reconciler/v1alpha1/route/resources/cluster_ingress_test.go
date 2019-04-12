/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/system"
	_ "github.com/knative/pkg/system/testing"
	"github.com/knative/serving/pkg/apis/networking"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestMakeClusterIngress_CorrectMetadata(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	ingressClass := "foo-ingress"
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			UID:       "1234-5678",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				Domain: "domain.com",
			},
		},
	}
	expected := metav1.ObjectMeta{
		Name: "route-1234-5678",
		Labels: map[string]string{
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: "test-ns",
		},
		Annotations: map[string]string{
			networking.IngressClassAnnotationKey: ingressClass,
		},
	}
	meta := MakeClusterIngress(r, &traffic.Config{Targets: targets}, ingressClass).ObjectMeta
	if diff := cmp.Diff(expected, meta); diff != "" {
		t.Errorf("Unexpected metadata (-want, +got): %v", diff)
	}
}

func TestMakeClusterIngressSpec_CorrectRules(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           100,
			},
			Active: true,
		}},
		"v1": {{
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           100,
			},
			Active: true,
		}},
	}
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				Domain: "domain.com",
			},
		},
	}
	expected := []netv1alpha1.ClusterIngressRule{{
		Hosts: []string{
			"domain.com",
			"test-route.test-ns.svc.cluster.local",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "v2-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
			}},
		},
	}, {
		Hosts: []string{"v1.domain.com"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "v1-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
			}},
		},
	}}
	rules := makeClusterIngressSpec(r, targets).Rules
	if diff := cmp.Diff(expected, rules); diff != "" {
		t.Errorf("Unexpected rules (-want, +got): %v", diff)
	}
}

func TestMakeClusterIngressSpec_CorrectVisibility(t *testing.T) {
	cases := []struct {
		name              string
		route             v1alpha1.Route
		expectedVisbility netv1alpha1.IngressVisibility
	}{{
		name: "public route",
		route: v1alpha1.Route{
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					Domain: "domain.com",
				},
			},
		},
		expectedVisbility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		name: "private route",
		route: v1alpha1.Route{
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					Domain: "local-route.default.svc.cluster.local",
				},
			},
		},
		expectedVisbility: netv1alpha1.IngressVisibilityClusterLocal,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := makeClusterIngressSpec(&c.route, nil).Visibility
			if diff := cmp.Diff(c.expectedVisbility, v); diff != "" {
				t.Errorf("Unexpected visibility (-want, +got): %s", diff)
			}
		})
	}
}

func TestGetRouteDomains_NamelessTargetDup(t *testing.T) {
	const base = "test-route.test-ns"
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				Domain: base,
			},
		},
	}
	expected := []string{
		base,
		"test-route.test-ns.svc.cluster.local",
	}
	domains := routeDomains("", r)
	if diff := cmp.Diff(expected, domains); diff != "" {
		t.Errorf("Unexpected domains  (-want, +got): %s", diff)
	}
}
func TestGetRouteDomains_NamelessTarget(t *testing.T) {
	const base = "domain.com"
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				Domain: base,
			},
		},
	}
	expected := []string{
		base,
		"test-route.test-ns.svc.cluster.local",
	}
	domains := routeDomains("", r)
	if diff := cmp.Diff(expected, domains); diff != "" {
		t.Errorf("Unexpected domains  (-want, +got): %s", diff)
	}
}

func TestGetRouteDomains_NamedTarget(t *testing.T) {
	const (
		name = "v1"
		base = "domain.com"
	)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				Domain: base,
			},
		},
	}
	expected := []string{"v1.domain.com"}
	domains := routeDomains(name, r)
	if diff := cmp.Diff(expected, domains); diff != "" {
		t.Errorf("Unexpected domains  (-want, +got): %s", diff)
	}
}

// One active target.
func TestMakeClusterIngressRule_Vanilla(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: true,
	}}
	domains := []string{"a.com", "b.org"}
	const ns = "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want, +got): %v", diff)
	}
}

// One active target and a target of zero percent.
func TestMakeClusterIngressRule_ZeroPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: true,
	}, {
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           0,
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want, +got): %v", diff)
	}
}

// Two active targets.
func TestMakeClusterIngressRule_TwoTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           80,
		},
		Active: true,
	}, {
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           20,
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	const ns = "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 80,
				}, {
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "new-revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
				}},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want, +got): %v", diff)
	}
}

// Inactive target.
func TestMakeClusterIngressRule_InactiveTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: false,
	}}
	domains := []string{"a.com", "b.org"}
	const ns = "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: system.Namespace(),
						ServiceName:      "activator-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				AppendHeaders: map[string]string{
					"knative-serving-revision":  "revision",
					"knative-serving-namespace": "test-ns",
				},
			}},
		},
	}
	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want, +got): %v", diff)
	}
}

// Two inactive targets.
func TestMakeClusterIngressRule_TwoInactiveTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           80,
		},
		Active: false,
	}, {
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           20,
		},
		Active: false,
	}}
	domains := []string{"a.com", "b.org"}
	const ns = "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: system.Namespace(),
						ServiceName:      "activator-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				AppendHeaders: map[string]string{
					"knative-serving-revision":  "revision",
					"knative-serving-namespace": "test-ns",
				},
			}},
		},
	}
	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want, +got): %v", diff)
	}
}

func TestMakeClusterIngressRule_ZeroPercentTargetInactive(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: true,
	}, {
		TrafficTarget: v1beta1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           0,
		},
		Active: false,
	}}
	domains := []string{"test.org"}
	const ns = "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want, +got): %v", diff)
	}
}
