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

package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmeta"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	apicfg "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	_ "knative.dev/pkg/system/testing"
	. "knative.dev/serving/pkg/testing/v1"
)

const (
	ns = "test-ns"

	testRouteName       = "test-route"
	testAnnotationValue = "test-annotation-value"
	testIngressClass    = "test-ingress"
)

func TestMakeIngressCorrectMetadata(t *testing.T) {
	const (
		ingressClass         = "ng-ingress"
		passdownIngressClass = "ok-ingress"
	)
	targets := map[string]traffic.RevisionTargets{}
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
	ia, err := MakeIngress(testContext(), r, &traffic.Config{Targets: targets}, nil, ingressClass)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ia.ObjectMeta) {
		t.Error("Unexpected metadata (-want, +got):", cmp.Diff(expected, ia.ObjectMeta))
	}
}

func TestMakeIngressWithRollout(t *testing.T) {
	const (
		ingressClass         = "ng-ingress"
		passdownIngressClass = "ok-ingress"
	)
	cfg := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "valhalla",
					LatestRevision:    ptr.Bool(true),
					Percent:           ptr.Int64(100),
					RevisionName:      "valhalla-01982",
				},
				ServiceName: "mia",
			}},
		},
	}
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
	ing, err := MakeIngress(testContext(), r, cfg, nil, ingressClass)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ing.ObjectMeta) {
		t.Error("Unexpected metadata (-want, +got):", cmp.Diff(expected, ing.ObjectMeta))
	}
}

func TestIngressNoKubectlAnnotation(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	r := Route(ns, testRouteName, WithRouteAnnotation(map[string]string{
		networking.IngressClassAnnotationKey: testIngressClass,
		corev1.LastAppliedConfigAnnotation:   testAnnotationValue,
	}), WithRouteUID("1234-5678"), WithURL)
	ing, err := MakeIngress(testContext(), r, &traffic.Config{Targets: targets}, nil, testIngressClass)
	if err != nil {
		t.Error("Unexpected error", err)
	}
	if v, ok := ing.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Errorf("Annotation %s = %q, want empty", corev1.LastAppliedConfigAnnotation, v)
	}
}

func TestMakeIngressSpecCorrectRules(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "gilberto",
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "jobim",
		}},
	}

	r := Route(ns, "test-route", WithURL)

	expected := []netv1alpha1.IngressRule{{
		Hosts: []string{
			"test-route." + ns,
			"test-route." + ns + ".svc",
			pkgnet.GetServiceHostname("test-route", ns),
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
		Visibility: netv1alpha1.IngressVisibilityClusterLocal,
	}, {
		Hosts: []string{
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
			"v1-test-route." + ns,
			"v1-test-route." + ns + ".svc",
			pkgnet.GetServiceHostname("v1-test-route", ns),
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
		Visibility: netv1alpha1.IngressVisibilityClusterLocal,
	}, {
		Hosts: []string{
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

	ci, err := makeIngressSpec(testContext(), r, nil, &traffic.Config{Targets: targets})
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
	}
}

func TestMakeIngressSpecCorrectRuleVisibility(t *testing.T) {
	cases := []struct {
		name               string
		route              *v1.Route
		targets            map[string]traffic.RevisionTargets
		serviceVisibility  map[string]netv1alpha1.IngressVisibility
		expectedVisibility map[netv1alpha1.IngressVisibility][]string
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
			}},
		},
		expectedVisibility: map[netv1alpha1.IngressVisibility][]string{
			netv1alpha1.IngressVisibilityClusterLocal: {"myroute.default", "myroute.default.svc", pkgnet.GetServiceHostname("myroute", "default")},
			netv1alpha1.IngressVisibilityExternalIP:   {"myroute.default.example.com"},
		},
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
			}},
		},
		serviceVisibility: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityClusterLocal,
		},
		expectedVisibility: map[netv1alpha1.IngressVisibility][]string{
			netv1alpha1.IngressVisibilityClusterLocal: {"myroute.default", "myroute.default.svc", pkgnet.GetServiceHostname("myroute", "default")},
		},
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
			}},
		},
		expectedVisibility: map[netv1alpha1.IngressVisibility][]string{
			netv1alpha1.IngressVisibilityClusterLocal: {"myroute.default", "myroute.default.svc", pkgnet.GetServiceHostname("myroute", "default")},
			netv1alpha1.IngressVisibilityExternalIP:   {"myroute.default.example.com"},
		},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ci, err := makeIngressSpec(testContext(), c.route, nil, &traffic.Config{
				Targets:    c.targets,
				Visibility: c.serviceVisibility,
			})
			if err != nil {
				t.Error("Unexpected error", err)
			}
			if len(c.expectedVisibility) != len(ci.Rules) {
				t.Errorf("Unexpected %d rules, saw %d", len(c.expectedVisibility), len(ci.Rules))
			}
			for _, rule := range ci.Rules {
				visibility := rule.Visibility
				if !cmp.Equal(c.expectedVisibility[visibility], rule.Hosts) {
					t.Errorf("Expected hosts %s for visibility %s, saw %s", c.expectedVisibility, visibility, rule.Hosts)
				}
			}
		})
	}
}

func TestMakeIngressSpecCorrectRulesWithTagBasedRouting(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "gilberto",
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "jobim",
		}},
	}

	r := Route(ns, "test-route", WithURL)

	expected := []netv1alpha1.IngressRule{{
		Hosts: []string{
			"test-route." + ns,
			"test-route." + ns + ".svc",
			pkgnet.GetServiceHostname("test-route", ns),
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Headers: map[string]netv1alpha1.HeaderMatch{
					network.TagHeaderName: {
						Exact: "v1",
					},
				},
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
			}, {
				AppendHeaders: map[string]string{
					network.DefaultRouteHeaderName: "true",
				},
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
		Visibility: netv1alpha1.IngressVisibilityClusterLocal,
	}, {
		Hosts: []string{
			"test-route." + ns + ".example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Headers: map[string]netv1alpha1.HeaderMatch{
					network.TagHeaderName: {
						Exact: "v1",
					},
				},
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
			}, {
				AppendHeaders: map[string]string{
					network.DefaultRouteHeaderName: "true",
				},
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
			"v1-test-route." + ns,
			"v1-test-route." + ns + ".svc",
			pkgnet.GetServiceHostname("v1-test-route", ns),
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				AppendHeaders: map[string]string{
					network.TagHeaderName: "v1",
				},
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
		Visibility: netv1alpha1.IngressVisibilityClusterLocal,
	}, {
		Hosts: []string{
			"v1-test-route." + ns + ".example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				AppendHeaders: map[string]string{
					network.TagHeaderName: "v1",
				},
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

	ctx := testContext()
	config.FromContext(ctx).Features.TagHeaderBasedRouting = apicfg.Enabled

	ci, err := makeIngressSpec(ctx, r, nil, &traffic.Config{Targets: targets})
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
	}
}

// One active target.
func TestMakeIngressRuleVanilla(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "chocolate",
	}}
	domains := []string{"a.com", "b.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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

	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

// One active target and a target of zero percent.
func TestMakeIngressRuleZeroPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "active-target",
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(0),
		},
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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

	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

// One active target and a target of 0 percent (nil is now impossible with defaulting to 0).
func TestMakeIngressRuleNilPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "active-target",
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(0),
		},
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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

	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

// Two active targets.
func TestMakeIngressRuleTwoTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(80),
		},
		ServiceName: "nigh",
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(20),
		},
		ServiceName: "death",
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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

	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

// Inactive target.
func TestMakeIngressRuleInactiveTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "strange-quark",
	}}
	domains := []string{"a.com", "b.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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
	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

// Two inactive targets.
func TestMakeIngressRuleTwoInactiveTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(80),
		},
		ServiceName: "up-quark",
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(20),
		},
		ServiceName: "down-quark",
	}}
	domains := []string{"a.com", "b.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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
	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

func TestMakeIngressRuleZeroPercentTargetInactive(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "apathy-sets-in",
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(0),
		},
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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

	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

func TestMakeIngressRuleImplicit0PercentTargetInactive(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           ptr.Int64(100),
		},
		ServiceName: "apathy-sets-in",
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           ptr.Int64(0),
		},
	}}
	domains := []string{"test.org"}
	rule := makeIngressRule(domains, ns, netv1alpha1.IngressVisibilityExternalIP, targets)
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

	if !cmp.Equal(expected, rule) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rule))
	}
}

func TestMakeIngressWithTLS(t *testing.T) {
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
			Rules: []netv1alpha1.IngressRule{},
			TLS:   tls,
		},
	}
	got, err := MakeIngress(testContext(), r, &traffic.Config{Targets: targets}, tls, ingressClass)
	if err != nil {
		t.Error("Unexpected error:", err)
	}

	if diff := cmp.Diff(expected, got); diff != "" {
		t.Error("Unexpected metadata (-want, +got):", diff)
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
		t.Error("Unexpected IngressTLS (-want, +got):", diff)
	}
}

func TestMakeIngressACMEChallenges(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "gilberto",
		}},
	}

	r := &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1.RouteStatus{
			RouteStatusFields: v1.RouteStatusFields{
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
			"test-route.test-ns",
			"test-route.test-ns.svc",
			pkgnet.GetServiceHostname("test-route", "test-ns"),
		},
		Visibility: netv1alpha1.IngressVisibilityClusterLocal,
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
			}}},
	}, {
		Hosts: []string{
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
			}}},
	}}

	ci, err := makeIngressSpec(testContext(), r, nil, &traffic.Config{Targets: targets}, acmeChallenge)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
	}

}

func TestMakeIngressFailToGenerateDomain(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "gilberto",
		}},
	}

	r := Route(ns, "test-route", WithURL)

	// Create a context that has a bad domain template.
	badContext := testContext()
	config.FromContext(badContext).Domain = &config.Domain{Domains: map[string]*config.LabelSelector{"example.com": {}}}
	config.FromContext(badContext).Network = &network.Config{
		DefaultIngressClass: "test-ingress-class",
		DomainTemplate:      "{{.UnknownField}}.{{.NonExistentField}}.{{.BadField}}",
		TagTemplate:         network.DefaultTagTemplate,
	}
	_, err := MakeIngress(badContext, r, &traffic.Config{Targets: targets}, nil, "")
	if err == nil {
		t.Error("Expected error, saw none")
	}
	if err != nil && !strings.Contains(err.Error(), "DomainTemplate") {
		t.Error("Expected DomainTemplate error, saw", err)
	}
}

func TestMakeIngressFailToGenerateTagHost(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "gilberto",
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
			ServiceName: "jobim",
		}},
	}

	r := Route(ns, "test-route", WithURL)

	// Create a context that has a bad domain template.
	badContext := testContext()
	config.FromContext(badContext).Domain = &config.Domain{Domains: map[string]*config.LabelSelector{"example.com": {}}}
	config.FromContext(badContext).Network = &network.Config{
		DefaultIngressClass: "test-ingress-class",
		DomainTemplate:      network.DefaultDomainTemplate,
		TagTemplate:         "{{.UnknownField}}.{{.NonExistentField}}.{{.BadField}}",
	}
	_, err := MakeIngress(badContext, r, &traffic.Config{Targets: targets}, nil, "")
	if err == nil {
		t.Error("Expected error, saw none")
	}
	if err != nil && !strings.Contains(err.Error(), "TagTemplate") {
		t.Error("Expected TagTemplate error, saw", err)
	}
}

func testContext() context.Context {
	ctx := context.Background()
	cfg := testConfig()
	configDefaults, _ := apicfg.NewDefaultsConfigFromMap(nil)
	return config.ToContext(apicfg.ToContext(ctx, &apicfg.Config{
		Defaults: configDefaults,
	}), cfg)
}
