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
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/networking"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"

	_ "knative.dev/pkg/system/testing"
)

const ns = "test-ns"

func getServiceVisibility() sets.String {
	return sets.NewString()
}

func TestMakeIngress_CorrectMetadata(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	ingressClass := "ng-ingress"
	passdownIngressClass := "ok-ingress"
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				serving.RouteLabelKey:          "try-to-override",
				serving.RouteNamespaceLabelKey: "try-to-override",
				"test-label":                   "foo",
			},
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: passdownIngressClass,
				"test-annotation":                    "bar",
			},
			UID: "1234-5678",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				URL: &apis.URL{
					Scheme: "http",
					Host:   "domain.com",
				},
			},
		},
	}
	expected := metav1.ObjectMeta{
		Name:      "test-route",
		Namespace: "test-ns",
		Labels: map[string]string{
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: "test-ns",
			"test-label":                   "foo",
		},
		Annotations: map[string]string{
			// Make sure to get passdownIngressClass instead of ingressClass
			networking.IngressClassAnnotationKey: passdownIngressClass,
			"test-annotation":                    "bar",
		},
		OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
	}
	ia, err := MakeIngress(getContext(), r, &traffic.Config{Targets: targets}, nil, getServiceVisibility(), ingressClass)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	ci := ia.(*netv1alpha1.Ingress)
	if !cmp.Equal(expected, ci.ObjectMeta) {
		t.Errorf("Unexpected metadata (-want, +got): %s", cmp.Diff(expected, ci.ObjectMeta))
	}
}

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
				URL: &apis.URL{
					Scheme: "http",
					Host:   "domain.com",
				},
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
	ia, err := MakeClusterIngress(getContext(), r, &traffic.Config{Targets: targets}, nil, getServiceVisibility(), ingressClass)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	ci := ia.(*netv1alpha1.ClusterIngress)
	if !cmp.Equal(expected, ci.ObjectMeta) {
		t.Errorf("Unexpected metadata (-want, +got): %s", cmp.Diff(expected, ci.ObjectMeta))
	}
}

func TestMakeClusterIngressSpec_CorrectRules(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "gilberto",
			Active:      true,
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "jobim",
			Active:      true,
		}},
	}

	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				URL: &apis.URL{
					Scheme: "http",
					Host:   "domain.com",
				},
			},
		},
	}

	expected := []netv1alpha1.IngressRule{{
		Hosts: []string{
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "gilberto",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "v2",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		Hosts: []string{
			"v1-test-route.test-ns.svc.cluster.local",
			"v1-test-route.test-ns.example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "jobim",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "v1",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}}

	ci, err := MakeIngressSpec(getContext(), r, nil, getServiceVisibility(), targets)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Errorf("Unexpected rules (-want, +got): %s", cmp.Diff(expected, ci.Rules))
	}
}

func TestMakeClusterIngressSpec_CorrectVisibility(t *testing.T) {
	cases := []struct {
		name               string
		route              v1alpha1.Route
		serviceVisibility  sets.String
		expectedVisibility netv1alpha1.IngressVisibility
	}{{
		name: "public route",
		route: v1alpha1.Route{
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					URL: &apis.URL{
						Scheme: "http",
						Host:   "domain.com",
					},
				},
			},
		},
		expectedVisibility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		name: "private route",
		route: v1alpha1.Route{
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					URL: &apis.URL{
						Scheme: "http",
						Host:   "local-route.default.svc.cluster.local",
					},
				},
			},
		},
		serviceVisibility:  sets.NewString(""),
		expectedVisibility: netv1alpha1.IngressVisibilityClusterLocal,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ci, err := MakeIngressSpec(getContext(), &c.route, nil, c.serviceVisibility, nil)
			if err != nil {
				t.Errorf("Unexpected error %v", err)
			}

			if !cmp.Equal(c.expectedVisibility, ci.Visibility) {
				t.Errorf("Unexpected visibility (-want, +got): %s", cmp.Diff(c.expectedVisibility, ci.Visibility))
			}
		})
	}
}

func TestMakeClusterIngressSpec_CorrectRuleVisibility(t *testing.T) {
	cases := []struct {
		name               string
		route              v1alpha1.Route
		targets            map[string]traffic.RevisionTargets
		serviceVisibility  sets.String
		expectedVisibility netv1alpha1.IngressVisibility
	}{{
		name: "public route",
		route: v1alpha1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "myroute",
			},
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					URL: &apis.URL{
						Scheme: "http",
						Host:   "domain.com",
					},
				},
			},
		},
		targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "config",
					RevisionName:      "v2",
					Percent:           ptr.Int64(100),
				},
				ServiceName: "gilberto",
				Active:      true,
			}},
		},
		expectedVisibility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		name: "private route",
		route: v1alpha1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "myroute",
			},
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					URL: &apis.URL{
						Scheme: "http",
						Host:   "local-route.default.svc.cluster.local",
					},
				},
			},
		},
		targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "config",
					RevisionName:      "v2",
					Percent:           ptr.Int64(100),
				},
				ServiceName: "gilberto",
				Active:      true,
			}},
		},
		serviceVisibility:  sets.NewString("myroute"),
		expectedVisibility: netv1alpha1.IngressVisibilityClusterLocal,
	}, {
		name: "unspecified route",
		route: v1alpha1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "myroute",
			},
			Status: v1alpha1.RouteStatus{
				RouteStatusFields: v1alpha1.RouteStatusFields{
					URL: &apis.URL{
						Scheme: "http",
						Host:   "local-route.default.svc.cluster.local",
					},
				},
			},
		},
		targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "config",
					RevisionName:      "v2",
					Percent:           ptr.Int64(100),
				},
				ServiceName: "gilberto",
				Active:      true,
			}},
		},
		serviceVisibility:  getServiceVisibility(),
		expectedVisibility: netv1alpha1.IngressVisibilityExternalIP,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ci, err := MakeIngressSpec(getContext(), &c.route, nil, c.serviceVisibility, c.targets)
			if err != nil {
				t.Errorf("Unexpected error %v", err)
			}

			if !cmp.Equal(c.expectedVisibility, ci.Rules[0].Visibility) {
				t.Errorf("Unexpected visibility (-want, +got): %s", cmp.Diff(c.expectedVisibility, ci.Rules[0].Visibility))
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
				URL: &apis.URL{
					Scheme: "http",
					Host:   base,
				},
			},
		},
	}
	expected := []string{
		"test-route.test-ns.svc.cluster.local",
		"test-route.test-ns.example.com",
	}
	domains, err := routeDomains(getContext(), "", r, false)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if !cmp.Equal(expected, domains) {
		t.Errorf("Unexpected domains (-want, +got): %s", cmp.Diff(expected, domains))
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
				URL: &apis.URL{
					Scheme: "http",
					Host:   base,
				},
			},
		},
	}
	expected := []string{
		"test-route.test-ns.svc.cluster.local",
		"test-route.test-ns.example.com",
	}
	domains, err := routeDomains(getContext(), "", r, false)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if !cmp.Equal(expected, domains) {
		t.Errorf("Unexpected domains (-want, +got): %s", cmp.Diff(expected, domains))
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
				URL: &apis.URL{
					Scheme: "http",
					Host:   base,
				},
			},
		},
	}
	expected := []string{
		"v1-test-route.test-ns.svc.cluster.local",
		"v1-test-route.test-ns.example.com",
	}
	domains, err := routeDomains(getContext(), name, r, false)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if !cmp.Equal(expected, domains) {
		t.Errorf("Unexpected domains (-want, +got): %s", cmp.Diff(expected, domains))
	}
}

// One active target.
func TestMakeClusterIngressRule_Vanilla(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "chocolate",
		Active:      true,
	}}
	domains := []string{"a.com", "b.org"}
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "chocolate",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

// One active target and a target of zero percent.
func TestMakeClusterIngressRule_ZeroPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "active-target",
		Active:      true,
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(0),
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "active-target",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

// One active target and a target of nil (implied zero) percent.
func TestMakeClusterIngressRule_NilPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "active-target",
		Active:      true,
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           nil,
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "active-target",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

// Two active targets.
func TestMakeClusterIngressRule_TwoTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(80),
		},
		ServiceName: "nigh",
		Active:      true,
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(20),
		},
		ServiceName: "death",
		Active:      true,
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "nigh",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 80,
					AppendHeaders: map[string]string{
						"Knative-Serving-Namespace": "test-ns",
						"Knative-Serving-Revision":  "revision",
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "death",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
					AppendHeaders: map[string]string{
						"Knative-Serving-Namespace": "test-ns",
						"Knative-Serving-Revision":  "new-revision",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

// Inactive target.
func TestMakeClusterIngressRule_InactiveTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "strange-quark",
		Active:      false,
	}}
	domains := []string{"a.com", "b.org"}
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      targets[0].ServiceName,
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}
	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

// Two inactive targets.
func TestMakeClusterIngressRule_TwoInactiveTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(80),
		},
		ServiceName: "up-quark",
		Active:      false,
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(20),
		},
		ServiceName: "down-quark",
		Active:      false,
	}}
	domains := []string{"a.com", "b.org"}
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      targets[0].ServiceName,
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 80,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      targets[1].ServiceName,
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "new-revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}
	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

func TestMakeClusterIngressRule_ZeroPercentTargetInactive(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "apathy-sets-in",
		Active:      true,
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(0),
		},
		Active: false,
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "apathy-sets-in",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

func TestMakeClusterIngressRule_NilPercentTargetInactive(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "apathy-sets-in",
		Active:      true,
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           nil,
		},
		Active: false,
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "apathy-sets-in",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": "test-ns",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(&expected, rule) {
		t.Errorf("Unexpected rule (-want, +got): %s", cmp.Diff(&expected, rule))
	}
}

func TestMakeClusterIngress_WithTLS(t *testing.T) {
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
				URL: &apis.URL{
					Host: "domain.com",
				},
			},
		},
	}
	tls := []netv1alpha1.IngressTLS{
		{
			Hosts:             []string{"*.default.domain.com"},
			PrivateKey:        "tls.key",
			SecretName:        "secret",
			ServerCertificate: "tls.crt",
		},
	}
	expected := &netv1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "route-1234-5678",
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
			},
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		},
		Spec: netv1alpha1.IngressSpec{
			Rules:      []netv1alpha1.IngressRule{},
			TLS:        tls,
			Visibility: netv1alpha1.IngressVisibilityExternalIP,
		},
	}
	got, err := MakeClusterIngress(getContext(), r, &traffic.Config{Targets: targets}, tls, getServiceVisibility(), ingressClass)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if diff := cmp.Diff(expected, got); diff != "" {
		t.Errorf("Unexpected metadata (-want, +got): %v", diff)
	}
}

func TestMakeClusterIngressTLS(t *testing.T) {
	cert := &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-1234",
			Namespace: system.Namespace(),
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   []string{"test.default.example.com", "v1.test.default.example.com"},
			SecretName: "route-1234",
		},
	}
	want := netv1alpha1.IngressTLS{
		Hosts:           []string{"test.default.example.com", "v1.test.default.example.com"},
		SecretName:      "route-1234",
		SecretNamespace: system.Namespace(),
	}
	hostNames := []string{"test.default.example.com", "v1.test.default.example.com"}
	got := MakeIngressTLS(cert, hostNames)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected IngressTLS (-want, +got): %v", diff)
	}
}

func getContext() context.Context {
	ctx := context.Background()
	cfg := testConfig()
	return config.ToContext(ctx, cfg)
}
