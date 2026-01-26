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
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netcfg "knative.dev/networking/pkg/config"
	netheader "knative.dev/networking/pkg/http/header"
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

	emptyRollout = "{}"

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
			networking.RolloutAnnotationKey:      emptyRollout,
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

func TestMakeIngressWithTaggedRollout(t *testing.T) {
	const ingressClass = "ng-ingress"

	cfg := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			"tagged": {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "thor",
					LatestRevision:    ptr.Bool(true),
					Percent:           ptr.Int64(100),
					RevisionName:      "thor-02020",
				},
			}},
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "valhalla",
					LatestRevision:    ptr.Bool(true),
					Percent:           ptr.Int64(100),
					RevisionName:      "valhalla-01982",
				},
			}},
		},
	}
	r := Route(ns, "test-route", WithRouteLabel(map[string]string{
		serving.RouteLabelKey:          "try-to-override",
		serving.RouteNamespaceLabelKey: "try-to-override",
		"test-label":                   "foo",
	}), WithRouteAnnotation(map[string]string{
		networking.IngressClassAnnotationKey: ingressClass,
		"test-annotation":                    "bar",
	}), WithRouteUID("1234-5678"), WithURL)

	wantMeta := metav1.ObjectMeta{
		Name:      "test-route",
		Namespace: ns,
		Labels: map[string]string{
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: ns,
			"test-label":                   "foo",
		},
		Annotations: map[string]string{
			networking.IngressClassAnnotationKey: ingressClass,
			networking.RolloutAnnotationKey:      `{"configurations":[{"configurationName":"valhalla","percent":100,"revisions":[{"revisionName":"valhalla-01982","percent":100}],"stepParams":{}},{"configurationName":"thor","tag":"tagged","percent":100,"revisions":[{"revisionName":"thor-02020","percent":100}],"stepParams":{}}]}`,
			"test-annotation":                    "bar",
		},
		OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
	}
	ing, err := MakeIngress(testContext(), r, cfg, nil, ingressClass)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(wantMeta, ing.ObjectMeta) {
		t.Error("Unexpected metadata (-want, +got):", cmp.Diff(wantMeta, ing.ObjectMeta))
	}
}

func TestMakeIngressWithActualRollout(t *testing.T) {
	const ingressClass = "ng-ingress"
	ro := &traffic.Rollout{
		Configurations: []*traffic.ConfigurationRollout{{
			ConfigurationName: "rune",
			Percent:           1,
			Revisions: []traffic.RevisionRollout{{
				RevisionName: "rune-01911",
				Percent:      1,
			}},
		}, {
			ConfigurationName: "valhalla",
			Percent:           99,
			Revisions: []traffic.RevisionRollout{{
				RevisionName: "valhalla-01981",
				Percent:      41,
			}, {
				RevisionName: "valhalla-01982",
				Percent:      68,
			}},
		}, {
			ConfigurationName: "rune",
			Percent:           1,
			Revisions: []traffic.RevisionRollout{{
				RevisionName: "rune-01911",
				Percent:      1,
			}},
		}, {
			ConfigurationName: "thor",
			Tag:               "hammer",
			Percent:           80,
			Revisions: []traffic.RevisionRollout{{
				RevisionName: "thor-02018",
				Percent:      60,
			}, {
				RevisionName: "thor-02019",
				Percent:      15,
			}, {
				RevisionName: "thor-02020",
				Percent:      5,
			}},
		}},
	}
	cfg := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			"hammer": {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "thor",
					LatestRevision:    ptr.Bool(true),
					Percent:           ptr.Int64(80),
					RevisionName:      "thor-02020",
				},
			}, {
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "thor",
					LatestRevision:    ptr.Bool(false),
					Percent:           ptr.Int64(20),
					RevisionName:      "thor-beta",
				},
			}},
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "rune",
					LatestRevision:    ptr.Bool(true),
					Percent:           ptr.Int64(1),
					RevisionName:      "rune-01911",
				},
			}, {
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "valhalla",
					LatestRevision:    ptr.Bool(true),
					Percent:           ptr.Int64(99),
					RevisionName:      "valhalla-01982",
				},
			}},
		},
	}
	r := Route(ns, "test-route", WithRouteAnnotation(map[string]string{
		networking.IngressClassAnnotationKey: ingressClass,
	}), WithRouteUID("1234-5678"), WithURL)

	wantMeta := metav1.ObjectMeta{
		Name:      "test-route",
		Namespace: ns,
		Labels: map[string]string{
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: ns,
		},
		Annotations: map[string]string{
			networking.IngressClassAnnotationKey: ingressClass,
			networking.RolloutAnnotationKey:      serializeRollout(context.Background(), ro),
		},
		OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
	}
	ing, err := MakeIngressWithRollout(testContext(), r, cfg, ro, nil /*tls*/, ingressClass)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(wantMeta, ing.ObjectMeta) {
		t.Error("Unexpected metadata (-want, +got):", cmp.Diff(wantMeta, ing.ObjectMeta))
	}
	wantRules := []netv1alpha1.IngressRule{{
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
						ServiceName:      "rune-01911",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 1,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "rune-01911",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "valhalla-01981",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 41,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "valhalla-01981",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "valhalla-01982",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 68,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "valhalla-01982",
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
						ServiceName:      "rune-01911",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 1,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "rune-01911",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "valhalla-01981",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 41,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "valhalla-01981",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "valhalla-01982",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 68,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "valhalla-01982",
						"Knative-Serving-Namespace": ns,
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}, {
		Hosts: []string{
			"hammer-test-route." + ns,
			"hammer-test-route." + ns + ".svc",
			pkgnet.GetServiceHostname("hammer-test-route", ns),
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-02018",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 60,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-02018",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-02019",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 15,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-02019",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-02020",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 5,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-02020",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-beta",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-beta",
						"Knative-Serving-Namespace": ns,
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityClusterLocal,
	}, {
		Hosts: []string{
			"hammer-test-route." + ns + ".example.com",
		},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-02018",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 60,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-02018",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-02019",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 15,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-02019",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-02020",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 5,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-02020",
						"Knative-Serving-Namespace": ns,
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "thor-beta",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "thor-beta",
						"Knative-Serving-Namespace": ns,
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}}
	if got, want := ing.Spec.Rules, wantRules; !cmp.Equal(got, want) {
		t.Errorf("Rules mismatch: diff(-want,+got)\n%s", cmp.Diff(want, got))
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
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
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
						ServiceName:      "v2",
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
						ServiceName:      "v2",
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
						ServiceName:      "v1",
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
						ServiceName:      "v1",
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

	tc := &traffic.Config{Targets: targets}
	ro := tc.BuildRollout()
	ci, err := makeIngressSpec(testContext(), r, nil /*tls*/, tc, ro)
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
			}},
		},
		expectedVisibility: map[netv1alpha1.IngressVisibility][]string{
			netv1alpha1.IngressVisibilityClusterLocal: {"myroute.default", "myroute.default.svc", pkgnet.GetServiceHostname("myroute", "default")},
			netv1alpha1.IngressVisibilityExternalIP:   {"myroute.default.example.com"},
		},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := &traffic.Config{
				Targets:    c.targets,
				Visibility: c.serviceVisibility,
			}
			ro := tc.BuildRollout()
			ci, err := makeIngressSpec(testContext(), c.route, nil /*tls*/, tc, ro)
			if err != nil {
				t.Error("Unexpected error", err)
			}
			if len(c.expectedVisibility) != len(ci.Rules) {
				t.Errorf("|rules| = %d, want: %d", len(ci.Rules), len(c.expectedVisibility))
			}
			for _, rule := range ci.Rules {
				visibility := rule.Visibility
				if !cmp.Equal(c.expectedVisibility[visibility], rule.Hosts) {
					t.Errorf("Hosts for visibility[%s] = %v, want: %v", visibility, rule.Hosts, c.expectedVisibility)
				}
			}
		})
	}
}

func TestMakeIngressSpecWithAppendHeaders(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
				AppendHeaders: map[string]string{
					"Foo": "Bar",
				},
			},
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
						ServiceName:      "v2",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "v2",
						"Knative-Serving-Namespace": ns,
						"Foo":                       "Bar",
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
						ServiceName:      "v2",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "v2",
						"Knative-Serving-Namespace": ns,
						"Foo":                       "Bar",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}}

	tc := &traffic.Config{Targets: targets}
	ro := tc.BuildRollout()
	ci, err := makeIngressSpec(testContext(), r, nil /*tls*/, tc, ro)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
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
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
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
					netheader.RouteTagKey: {
						Exact: "v1",
					},
				},
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "v1",
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
					netheader.DefaultRouteKey: "true",
				},
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "v2",
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
					netheader.RouteTagKey: {
						Exact: "v1",
					},
				},
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "v1",
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
					netheader.DefaultRouteKey: "true",
				},
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "v2",
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
					netheader.RouteTagKey: "v1",
				},
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "v1",
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
					netheader.RouteTagKey: "v1",
				},
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "v1",
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

	tc := &traffic.Config{Targets: targets}
	ro := tc.BuildRollout()
	ci, err := makeIngressSpec(ctx, r, nil /*tls*/, tc, ro)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
	}
}

// One active target with ExternalIP visibility creates one rule per domain.
func TestMakeIngressRuleVanilla(t *testing.T) {
	domains := sets.New("a.com", "b.org")
	targets := traffic.RevisionTargets{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision-shark",
			Percent:           ptr.Int64(100),
		},
	}}
	tc := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: targets,
		},
	}
	ro := tc.BuildRollout()
	rules := makeIngressRules(domains, ns,
		netv1alpha1.IngressVisibilityExternalIP, targets, ro.RolloutsByTag(traffic.DefaultTarget), false /* internal encryption */)

	// ExternalIP visibility creates one rule per host
	if len(rules) != 2 {
		t.Fatalf("Expected 2 rules (one per host), got %d", len(rules))
	}

	// Both rules should have the same structure, just different single hosts
	for _, rule := range rules {
		if len(rule.Hosts) != 1 {
			t.Errorf("Expected 1 host per rule, got %d", len(rule.Hosts))
		}
		if rule.Hosts[0] != "a.com" && rule.Hosts[0] != "b.org" {
			t.Errorf("Unexpected host: %s", rule.Hosts[0])
		}
		expectedPath := netv1alpha1.HTTPIngressPath{
			Splits: []netv1alpha1.IngressBackendSplit{{
				IngressBackend: netv1alpha1.IngressBackend{
					ServiceNamespace: ns,
					ServiceName:      "revision-shark",
					ServicePort:      intstr.FromInt(80),
				},
				Percent: 100,
				AppendHeaders: map[string]string{
					"Knative-Serving-Revision":  "revision-shark",
					"Knative-Serving-Namespace": ns,
				},
			}},
		}
		if !cmp.Equal(expectedPath, rule.HTTP.Paths[0]) {
			t.Error("Unexpected path (-want, +got):", cmp.Diff(expectedPath, rule.HTTP.Paths[0]))
		}
		if rule.Visibility != netv1alpha1.IngressVisibilityExternalIP {
			t.Errorf("Expected ExternalIP visibility, got %v", rule.Visibility)
		}
	}
}

// One active target and a target of zero percent.
func TestMakeIngressRuleZeroPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision-dolphin",
			Percent:           ptr.Int64(100),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision-orca",
			Percent:           ptr.Int64(0),
		},
	}}
	domains := sets.New("test.org")
	tc := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: targets,
		},
	}
	ro := tc.BuildRollout()
	rules := makeIngressRules(domains, ns,
		netv1alpha1.IngressVisibilityExternalIP, targets, ro.RolloutsByTag(traffic.DefaultTarget), false /* internal encryption */)

	if len(rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules))
	}

	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "revision-dolphin",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "revision-dolphin",
						"Knative-Serving-Namespace": ns,
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(expected, rules[0]) {
		t.Error("Unexpected rule (-want, +got):", cmp.Diff(expected, rules[0]))
	}
}

// Two active targets.
func TestMakeIngressRuleTwoTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision-beluga",
			Percent:           ptr.Int64(80),
		},
	}, {
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision-narwhal",
			Percent:           ptr.Int64(20),
		},
	}}
	tc := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			"a-tag": targets,
		},
	}
	ro := tc.BuildRollout()
	domains := sets.New("test.org")
	rules := makeIngressRules(domains, ns, netv1alpha1.IngressVisibilityExternalIP,
		targets, ro.RolloutsByTag("a-tag"), false /* internal encryption */)

	if len(rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules))
	}

	expected := netv1alpha1.IngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{{
				Splits: []netv1alpha1.IngressBackendSplit{{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "revision-beluga",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 80,
					AppendHeaders: map[string]string{
						"Knative-Serving-Namespace": ns,
						"Knative-Serving-Revision":  "revision-beluga",
					},
				}, {
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      "new-revision-narwhal",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
					AppendHeaders: map[string]string{
						"Knative-Serving-Namespace": ns,
						"Knative-Serving-Revision":  "new-revision-narwhal",
					},
				}},
			}},
		},
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
	}

	if !cmp.Equal(expected, rules[0]) {
		t.Errorf("Unexpected rule (-want, +got):\n%s", cmp.Diff(expected, rules[0]))
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
				networking.RolloutAnnotationKey:      emptyRollout,
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

func TestMakeIngressWithHTTPOption(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{}
	ingressClass := "foo-ingress"
	tls := []netv1alpha1.IngressTLS{{
		Hosts:      []string{"*.default.domain.com"},
		SecretName: "secret",
	}}
	tests := []struct {
		name                 string
		httpOptionAnnotation string
		wantOption           netv1alpha1.HTTPOption
		wantError            bool
	}{{
		name:                 "Route annotation HTTPOption enabled",
		httpOptionAnnotation: "Enabled",
		wantOption:           netv1alpha1.HTTPOptionEnabled,
	}, {
		name:       "No HTTPOption annotation",
		wantOption: netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                 "Incorrect HTTPOption annotation",
		httpOptionAnnotation: "INCORRECT",
		wantError:            true,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			annotations := map[string]string{
				networking.HTTPOptionAnnotationKey: tc.httpOptionAnnotation,
			}
			r := Route(ns, "test-route", WithURL, WithRouteAnnotation(annotations))
			got, err := MakeIngress(testContextWithHTTPOption(), r, &traffic.Config{Targets: targets}, tls, ingressClass)
			if (err != nil) != tc.wantError {
				t.Fatalf("MakeIngress() error = %v, WantErr %v", err, tc.wantError)
			}
			if tc.wantError {
				return
			}
			if diff := cmp.Diff(tc.wantOption, got.Spec.HTTPOption); diff != "" {
				t.Error("Unexpected Ingress (-want, +got):", diff)
			}
		})
	}
}

func TestMakeIngressWithActivatorCA(t *testing.T) {
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           ptr.Int64(100),
			},
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
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
						ServiceName:      "v2",
						ServicePort:      intstr.FromInt(networking.ServiceHTTPSPort),
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
						ServiceName:      "v2",
						ServicePort:      intstr.FromInt(networking.ServiceHTTPSPort),
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
						ServiceName:      "v1",
						ServicePort:      intstr.FromInt(networking.ServiceHTTPSPort),
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
						ServiceName:      "v1",
						ServicePort:      intstr.FromInt(networking.ServiceHTTPSPort),
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

	tc := &traffic.Config{Targets: targets}
	ro := tc.BuildRollout()
	ci, err := makeIngressSpec(testContextWithActivatorCA(), r, nil /*tls*/, tc, ro)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
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
						ServiceName:      "v2",
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
						ServiceName:      "v2",
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
	}}

	tc := &traffic.Config{
		Targets: targets,
	}
	ro := tc.BuildRollout()

	ci, err := makeIngressSpec(testContext(), r, nil /*tls*/, tc, ro, acmeChallenge)
	if err != nil {
		t.Error("Unexpected error", err)
	}

	if !cmp.Equal(expected, ci.Rules) {
		t.Error("Unexpected rules (-want, +got):", cmp.Diff(expected, ci.Rules))
	}
}

func TestMakeIngressACMEChallengesWithTrafficTags(t *testing.T) {
	// Test that ACME challenges don't create duplicate domains across traffic tag rules
	targets := map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
		}},
		"blue": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
		}},
		"green": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
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
					Host:   "test-route.test-ns.example.com",
				},
			},
		},
	}

	// Three ACME challenges: one for default and one for each tag
	acmeChallenges := []netv1alpha1.HTTP01Challenge{
		{
			ServiceNamespace: "test-ns",
			ServiceName:      "cm-solver",
			ServicePort:      intstr.FromInt(8090),
			URL: &apis.URL{
				Scheme: "http",
				Path:   "/.well-known/acme-challenge/token-default",
				Host:   "test-route.test-ns.example.com",
			},
		},
		{
			ServiceNamespace: "test-ns",
			ServiceName:      "cm-solver-blue",
			ServicePort:      intstr.FromInt(8090),
			URL: &apis.URL{
				Scheme: "http",
				Path:   "/.well-known/acme-challenge/token-blue",
				Host:   "blue-test-route.test-ns.example.com",
			},
		},
		{
			ServiceNamespace: "test-ns",
			ServiceName:      "cm-solver-green",
			ServicePort:      intstr.FromInt(8090),
			URL: &apis.URL{
				Scheme: "http",
				Path:   "/.well-known/acme-challenge/token-green",
				Host:   "green-test-route.test-ns.example.com",
			},
		},
	}

	tc := &traffic.Config{
		Targets: targets,
	}
	ro := tc.BuildRollout()

	ci, err := makeIngressSpec(testContext(), r, nil /*tls*/, tc, ro, acmeChallenges...)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	// Verify each ACME challenge path appears in exactly one rule
	acmePathsSeen := make(map[string]int) // path -> count across all rules
	for _, rule := range ci.Rules {
		if rule.Visibility != netv1alpha1.IngressVisibilityExternalIP || rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Path != "" && len(path.Path) > 0 {
				acmePathsSeen[path.Path]++
			}
		}
	}

	// Each ACME challenge path should appear exactly once
	expectedPaths := []string{
		"/.well-known/acme-challenge/token-default",
		"/.well-known/acme-challenge/token-blue",
		"/.well-known/acme-challenge/token-green",
	}
	for _, path := range expectedPaths {
		if count := acmePathsSeen[path]; count != 1 {
			t.Errorf("ACME challenge path %q appears in %d rules (expected 1)", path, count)
		}
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
		}},
	}

	r := Route(ns, "test-route", WithURL)

	// Create a context that has a bad domain template.
	badContext := testContext()
	config.FromContext(badContext).Domain = &config.Domain{Domains: map[string]config.DomainConfig{"example.com": {}}}
	config.FromContext(badContext).Network = &netcfg.Config{
		DefaultIngressClass: "test-ingress-class",
		DomainTemplate:      "{{.UnknownField}}.{{.NonExistentField}}.{{.BadField}}",
		TagTemplate:         netcfg.DefaultTagTemplate,
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
		}},
		"v1": {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           ptr.Int64(100),
			},
		}},
	}

	r := Route(ns, "test-route", WithURL)

	// Create a context that has a bad domain template.
	badContext := testContext()
	config.FromContext(badContext).Domain = &config.Domain{Domains: map[string]config.DomainConfig{"example.com": {}}}
	config.FromContext(badContext).Network = &netcfg.Config{
		DefaultIngressClass: "test-ingress-class",
		DomainTemplate:      netcfg.DefaultDomainTemplate,
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

func testContextWithHTTPOption() context.Context {
	cfg := testConfig()
	cfg.Network.HTTPProtocol = netcfg.HTTPRedirected
	return config.ToContext(context.Background(), cfg)
}

func testContextWithActivatorCA() context.Context {
	cfg := testConfig()
	cfg.Network.SystemInternalTLS = netcfg.EncryptionEnabled
	return config.ToContext(context.Background(), cfg)
}
