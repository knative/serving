/*
Copyright 2020 The Knative Authors

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

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	netapi "knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

func TestMakeIngress(t *testing.T) {
	for _, tc := range []struct {
		name           string
		dm             v1beta1.DomainMapping
		want           netv1alpha1.Ingress
		tls            []netv1alpha1.IngressTLS
		acmeChallenges []netv1alpha1.HTTP01Challenge
	}{{
		name: "basic",
		dm: v1beta1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				UID:       types.UID("the-uid"),
				Annotations: map[string]string{
					"some.annotation":                  "some.value",
					corev1.LastAppliedConfigAnnotation: "blah",
				},
			},
			Spec: v1beta1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
			},
		},
		want: netv1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					netapi.IngressClassAnnotationKey: "the-ingress-class",
					"some.annotation":                "some.value",
				},
			},
			Spec: netv1alpha1.IngressSpec{
				HTTPOption: netv1alpha1.HTTPOptionEnabled,
				Rules: []netv1alpha1.IngressRule{{
					Hosts:      []string{"mapping.com"},
					Visibility: netv1alpha1.IngressVisibilityExternalIP,
					HTTP: &netv1alpha1.HTTPIngressRuleValue{
						Paths: []netv1alpha1.HTTPIngressPath{{
							RewriteHost: "the-rewrite-host",
							Splits: []netv1alpha1.IngressBackendSplit{{
								Percent: 100,
								AppendHeaders: map[string]string{
									netheader.OriginalHostKey: "mapping.com",
								},
								IngressBackend: netv1alpha1.IngressBackend{
									ServiceName:      "the-target-svc",
									ServiceNamespace: "the-namespace",
									ServicePort:      intstr.FromInt(80),
								},
							}},
						}},
					},
				}},
			},
		},
	}, {
		name: "tls",
		dm: v1beta1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				UID:       types.UID("the-uid"),
				Annotations: map[string]string{
					"some.annotation":                  "some.value",
					corev1.LastAppliedConfigAnnotation: "blah",
				},
			},
			Spec: v1beta1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
			},
		},
		tls: []netv1alpha1.IngressTLS{{
			Hosts:      []string{"mapping.com"},
			SecretName: "secret",
		}},
		want: netv1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					netapi.IngressClassAnnotationKey: "the-ingress-class",
					"some.annotation":                "some.value",
				},
			},
			Spec: netv1alpha1.IngressSpec{
				HTTPOption: netv1alpha1.HTTPOptionEnabled,
				Rules: []netv1alpha1.IngressRule{{
					Hosts:      []string{"mapping.com"},
					Visibility: netv1alpha1.IngressVisibilityExternalIP,
					HTTP: &netv1alpha1.HTTPIngressRuleValue{
						Paths: []netv1alpha1.HTTPIngressPath{{
							RewriteHost: "the-rewrite-host",
							Splits: []netv1alpha1.IngressBackendSplit{{
								Percent: 100,
								AppendHeaders: map[string]string{
									netheader.OriginalHostKey: "mapping.com",
								},
								IngressBackend: netv1alpha1.IngressBackend{
									ServiceName:      "the-target-svc",
									ServiceNamespace: "the-namespace",
									ServicePort:      intstr.FromInt(80),
								},
							}},
						}},
					},
				}},
				TLS: []netv1alpha1.IngressTLS{{
					Hosts:      []string{"mapping.com"},
					SecretName: "secret",
				}},
			},
		},
	}, {
		name: "challenges",
		dm: v1beta1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				UID:       types.UID("the-uid"),
				Annotations: map[string]string{
					"some.annotation":                  "some.value",
					corev1.LastAppliedConfigAnnotation: "blah",
				},
			},
			Spec: v1beta1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
			},
		},
		acmeChallenges: []netv1alpha1.HTTP01Challenge{{
			ServiceNamespace: "test-ns",
			ServiceName:      "cm-solver",
			ServicePort:      intstr.FromInt(8090),
			URL: &apis.URL{
				Scheme: "http",
				Path:   "/.well-known/acme-challenge/challenge-token",
				Host:   "mapping.com",
			},
		}},
		want: netv1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					netapi.IngressClassAnnotationKey: "the-ingress-class",
					"some.annotation":                "some.value",
				},
			},
			Spec: netv1alpha1.IngressSpec{
				HTTPOption: netv1alpha1.HTTPOptionEnabled,
				Rules: []netv1alpha1.IngressRule{{
					Hosts:      []string{"mapping.com"},
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
							RewriteHost: "the-rewrite-host",
							Splits: []netv1alpha1.IngressBackendSplit{{
								Percent: 100,
								AppendHeaders: map[string]string{
									netheader.OriginalHostKey: "mapping.com",
								},
								IngressBackend: netv1alpha1.IngressBackend{
									ServiceName:      "the-target-svc",
									ServiceNamespace: "the-namespace",
									ServicePort:      intstr.FromInt(80),
								},
							}},
						}},
					},
				}},
			},
		},
	}, {
		name: "challenge with non-matching host",
		dm: v1beta1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				UID:       types.UID("the-uid"),
			},
			Spec: v1beta1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "the-namespace",
					Name:      "the-name",
				},
			},
		},
		acmeChallenges: []netv1alpha1.HTTP01Challenge{{
			ServiceNamespace: "test-ns",
			ServiceName:      "cm-solver",
			ServicePort:      intstr.FromInt(8090),
			URL: &apis.URL{
				Scheme: "http",
				Path:   "/.well-known/acme-challenge/token",
				Host:   "truncated.example.com", // Different from mapping.com
			},
		}},
		want: netv1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					netapi.IngressClassAnnotationKey: "the-ingress-class",
				},
			},
			Spec: netv1alpha1.IngressSpec{
				HTTPOption: netv1alpha1.HTTPOptionEnabled,
				Rules: []netv1alpha1.IngressRule{{
					Hosts:      []string{"mapping.com"},
					Visibility: netv1alpha1.IngressVisibilityExternalIP,
					HTTP: &netv1alpha1.HTTPIngressRuleValue{
						Paths: []netv1alpha1.HTTPIngressPath{{
							RewriteHost: "the-rewrite-host",
							Splits: []netv1alpha1.IngressBackendSplit{{
								Percent: 100,
								AppendHeaders: map[string]string{
									netheader.OriginalHostKey: "mapping.com",
								},
								IngressBackend: netv1alpha1.IngressBackend{
									ServiceName:      "the-target-svc",
									ServiceNamespace: "the-namespace",
									ServicePort:      intstr.FromInt(80),
								},
							}},
						}},
					},
				}, {
					Hosts:      []string{"truncated.example.com"},
					Visibility: netv1alpha1.IngressVisibilityExternalIP,
					HTTP: &netv1alpha1.HTTPIngressRuleValue{
						Paths: []netv1alpha1.HTTPIngressPath{{
							Path: "/.well-known/acme-challenge/token",
							Splits: []netv1alpha1.IngressBackendSplit{{
								IngressBackend: netv1alpha1.IngressBackend{
									ServiceNamespace: "test-ns",
									ServiceName:      "cm-solver",
									ServicePort:      intstr.FromInt(8090),
								},
								Percent: 100,
							}},
						}},
					},
				}},
			},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.Labels = kmeta.UnionMaps(tc.dm.Labels, map[string]string{
				serving.DomainMappingUIDLabelKey:       "the-uid",
				serving.DomainMappingNamespaceLabelKey: "the-namespace",
			})
			tc.want.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(&tc.dm)}
			got := *MakeIngress(&tc.dm,
				"the-target-svc", "the-rewrite-host", "the-ingress-class",
				netv1alpha1.HTTPOptionEnabled,
				tc.tls, tc.acmeChallenges...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unexpected Ingress (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestMakeIngressWithHTTPPaths(t *testing.T) {
	dm := &v1beta1.DomainMapping{ObjectMeta: metav1.ObjectMeta{
		Name:      "mapping.com",
		Namespace: "default",
	}}
	source := []netv1alpha1.HTTPIngressPath{{
		RewriteHost: "old.internal.example",
		AppendHeaders: map[string]string{
			"path-header": "preserved",
		},
		Splits: []netv1alpha1.IngressBackendSplit{{
			IngressBackend: netv1alpha1.IngressBackend{
				ServiceNamespace: "default",
				ServiceName:      "app-00001",
				ServicePort:      intstr.FromInt(443),
			},
			Percent: 75,
			AppendHeaders: map[string]string{
				"Knative-Serving-Revision": "app-00001",
				netheader.OriginalHostKey:  "stale.example",
			},
		}, {
			IngressBackend: netv1alpha1.IngressBackend{
				ServiceNamespace: "default",
				ServiceName:      "app-00002",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 25,
		}},
	}}

	got := MakeIngressWithHTTPPaths(dm, source, "example.net/ingress", netv1alpha1.HTTPOptionEnabled, nil)
	paths := got.Spec.Rules[0].HTTP.Paths
	if got, want := paths[0].RewriteHost, ""; got != want {
		t.Errorf("RewriteHost = %q, want %q", got, want)
	}
	if got, want := paths[0].Splits[0].ServicePort, intstr.FromInt(443); got != want {
		t.Errorf("TLS backend port = %v, want %v", got, want)
	}
	if got, want := paths[0].Splits[0].AppendHeaders["Knative-Serving-Revision"], "app-00001"; got != want {
		t.Errorf("revision header = %q, want %q", got, want)
	}
	for i := range paths[0].Splits {
		if got, want := paths[0].Splits[i].AppendHeaders[netheader.OriginalHostKey], dm.Name; got != want {
			t.Errorf("split %d original host = %q, want %q", i, got, want)
		}
	}

	paths[0].AppendHeaders["path-header"] = "changed"
	paths[0].Splits[0].AppendHeaders["Knative-Serving-Revision"] = "changed"
	if got, want := source[0].AppendHeaders["path-header"], "preserved"; got != want {
		t.Errorf("source path header was mutated: got %q, want %q", got, want)
	}
	if got, want := source[0].Splits[0].AppendHeaders["Knative-Serving-Revision"], "app-00001"; got != want {
		t.Errorf("source split header was mutated: got %q, want %q", got, want)
	}
}
