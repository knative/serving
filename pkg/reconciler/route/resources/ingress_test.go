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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/networking"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"

	_ "knative.dev/pkg/system/testing"
	. "knative.dev/serving/pkg/testing/v1alpha1"
)

const (
	ns = "test-ns"

	testRouteName       = "test-route"
	testAnnotationValue = "test-annotation-value"
	testIngressClass    = "test-ingress"
)

func getServiceVisibility() sets.String {
	return sets.NewString()
}

func TestMakeIngress_CorrectMetadata(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	ingressClass := "ng-ingress"
	passdownIngressClass := "ok-ingress"
	r := Route(ns, "test-route", WithRouteLabel(map[string]string{
		serving.RouteLabelKey:          "try-to-override",
		serving.RouteNamespaceLabelKey: "try-to-override",
		"test-label":                   "foo",
	}), WithRouteAnnotation(map[string]string{
		networking.IngressClassAnnotationKey: passdownIngressClass,
		"test-annotation":                    "bar",
	}), WithRouteUID("1234-5678"), WithURL)
	expected := metav1.ObjectMeta{
		Name:      "test-route",
		Namespace: ns,
		Labels: map[string]string{
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: ns,
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

	if !cmp.Equal(expected, ia.ObjectMeta) {
		t.Errorf("Unexpected metadata (-want, +got): %s", cmp.Diff(expected, ia.ObjectMeta))
	}
}

func TestIngress_NoKubectlAnnotation(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	r := Route(ns, testRouteName, WithRouteAnnotation(map[string]string{
		networking.IngressClassAnnotationKey: testIngressClass,
		corev1.LastAppliedConfigAnnotation:   testAnnotationValue,
	}), WithRouteUID("1234-5678"), WithURL)
	ia, err := MakeIngress(getContext(), r, &traffic.Config{Targets: targets}, nil, getServiceVisibility(), testIngressClass)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if v, ok := ia.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Errorf("Annotation %s = %q, want empty", corev1.LastAppliedConfigAnnotation, v)
	}
}

func TestMakeIngressSpec_CorrectRules(t *testing.T) {
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

	r := Route(ns, "test-route", WithURL)

	expected := []netv1alpha1.IngressRule{{
		Hosts: []string{
			"test-route." + ns + ".svc.cluster.local",
			"test-route." + ns + ".example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "gilberto",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "v2",
						"Knative-Serving-Namespace": ns,
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		Hosts: []string{
			"v1-test-route." + ns + ".svc.cluster.local",
			"v1-test-route." + ns + ".example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "jobim",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "v1",
						"Knative-Serving-Namespace": ns,
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

func TestMakeIngressSpec_CorrectVisibility(t *testing.T) {
	cases := []struct {
		name               string
		route              *v1alpha1.Route
		serviceVisibility  sets.String
		expectedVisibility netv1alpha1.IngressVisibility
	}{{
		name:  "public route",
		route: Route("", "", WithURL),

		expectedVisibility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		name:               "private route",
		route:              Route("", "", WithAddress),
		serviceVisibility:  sets.NewString(""),
		expectedVisibility: netv1alpha1.IngressVisibilityClusterLocal,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ci, err := MakeIngressSpec(getContext(), c.route, nil, c.serviceVisibility, nil)
			if err != nil {
				t.Errorf("Unexpected error %v", err)
			}

			if !cmp.Equal(c.expectedVisibility, ci.Visibility) {
				t.Errorf("Unexpected visibility (-want, +got): %s", cmp.Diff(c.expectedVisibility, ci.Visibility))
			}
		})
	}
}

func TestMakeIngressSpec_CorrectRuleVisibility(t *testing.T) {
	cases := []struct {
		name               string
		route              *v1alpha1.Route
		targets            map[string]traffic.RevisionTargets
		serviceVisibility  sets.String
		expectedVisibility netv1alpha1.IngressVisibility
	}{{
		name:  "public route",
		route: Route("default", "myroute", WithURL),
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
		name:  "private route",
		route: Route("default", "myroute", WithLocalDomain),
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
		name:  "unspecified route",
		route: Route("default", "myroute", WithLocalDomain),
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
			ci, err := MakeIngressSpec(getContext(), c.route, nil, c.serviceVisibility, c.targets)
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
	r := Route("test-ns", "test-route", WithURL)
	expected := []string{
		"test-route." + ns + ".svc.cluster.local",
		"test-route." + ns + ".example.com",
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
	r := Route("test-ns", "test-route", WithURL)
	expected := []string{
		"test-route." + ns + ".svc.cluster.local",
		"test-route." + ns + ".example.com",
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
	)
	r := Route("test-ns", "test-route", WithURL)
	expected := []string{

		"v1-test-route." + ns + ".svc.cluster.local",
		"v1-test-route." + ns + ".example.com",
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
func TestMakeIngressRule_Vanilla(t *testing.T) {
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
						ServiceNamespace: ns,
						ServiceName:      "chocolate",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": ns,
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
func TestMakeIngressRule_ZeroPercentTarget(t *testing.T) {
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
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "active-target",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": ns,
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
func TestMakeIngressRule_NilPercentTarget(t *testing.T) {
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
	rule := makeIngressRule(domains, ns, false, targets)
	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "active-target",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": ns,
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
func TestMakeIngressRule_TwoTargets(t *testing.T) {
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
						ServiceNamespace: ns,
						ServiceName:      "nigh",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 80,
					AppendHeaders: map[string]string{
						"Knative-Serving-Namespace": ns,
						"Knative-Serving-Revision":  "revision",
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "death",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
					AppendHeaders: map[string]string{
						"Knative-Serving-Namespace": ns,
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
func TestMakeIngressRule_InactiveTarget(t *testing.T) {
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
						"Knative-Serving-Namespace": ns,
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
func TestMakeIngressRule_TwoInactiveTargets(t *testing.T) {
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
						"Knative-Serving-Namespace": ns,
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
						"Knative-Serving-Namespace": ns,
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

func TestMakeIngressRule_ZeroPercentTargetInactive(t *testing.T) {
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
						ServiceNamespace: ns,
						ServiceName:      "apathy-sets-in",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": ns,
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

func TestMakeIngressRule_NilPercentTargetInactive(t *testing.T) {
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
						ServiceNamespace: ns,
						ServiceName:      "apathy-sets-in",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision",
						"Knative-Serving-Namespace": ns,
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

func TestMakeIngress_WithTLS(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	ingressClass := "foo-ingress"
	r := Route(ns, "test-route", WithRouteUID("1234-5678"), WithURL)
	tls := []netv1alpha1.IngressTLS{{
		Hosts:      []string{"*.default.domain.com"},
		SecretName: "secret",
	}}
	expected := &netv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: ns,
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
			},
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: ns,
			},
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
		},
		Spec: netv1alpha1.IngressSpec{
			Rules:      []netv1alpha1.IngressRule{},
			TLS:        tls,
			Visibility: netv1alpha1.IngressVisibilityExternalIP,
		},
	}
	got, err := MakeIngress(getContext(), r, &traffic.Config{Targets: targets}, tls, getServiceVisibility(), ingressClass)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if diff := cmp.Diff(expected, got); diff != "" {
		t.Errorf("Unexpected metadata (-want, +got): %v", diff)
	}
}

func TestMakeIngressTLS(t *testing.T) {
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

func TestMakeClusterIngress_ACMEChallenges(t *testing.T) {
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

	acmeChallenge := netv1alpha1.HTTP01Challenge{
		ServiceNamespace: "test-ns",
		ServiceName:      "cm-solver",
		ServicePort:      intstr.FromInt(8090),
		URL: &apis.URL{
			Scheme: "http",
			Path:   "/.well-known/acme-challenge/challenge-token",
			Host:   "test-route.test-ns.example.com",
		},
	}

	expected := []netv1alpha1.IngressRule{{
		Hosts: []string{
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.example.com",
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Path: "/.well-known/acme-challenge/challenge-token",
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "cm-solver",
						ServicePort:      intstr.FromInt(8090),
					},
					Percent: 100,
				}},
			}, {
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
			}}}}}

	ci, err := MakeIngressSpec(getContext(), r, nil, getServiceVisibility(), targets, acmeChallenge)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Errorf("Unexpected rules (-want, +got): %s", cmp.Diff(expected, ci.Rules))
	}

}

func getContext() context.Context {
	ctx := context.Background()
	cfg := testConfig()
	return config.ToContext(ctx, cfg)
}
