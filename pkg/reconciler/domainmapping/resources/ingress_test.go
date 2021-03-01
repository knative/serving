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
	"k8s.io/apimachinery/pkg/util/intstr"
	network "knative.dev/networking/pkg"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

func TestMakeIngress(t *testing.T) {
	for _, tc := range []struct {
		name           string
		dm             v1alpha1.DomainMapping
		want           netv1alpha1.Ingress
		tls            []netv1alpha1.IngressTLS
		acmeChallenges []netv1alpha1.HTTP01Challenge
	}{{
		name: "basic",
		dm: v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					"some.annotation":                  "some.value",
					corev1.LastAppliedConfigAnnotation: "blah",
				},
			},
			Spec: v1alpha1.DomainMappingSpec{
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
					"networking.knative.dev/ingress.class": "the-ingress-class",
					"some.annotation":                      "some.value",
				},
			},
			Spec: netv1alpha1.IngressSpec{
				Rules: []netv1alpha1.IngressRule{{
					Hosts:      []string{"mapping.com"},
					Visibility: netv1alpha1.IngressVisibilityExternalIP,
					HTTP: &netv1alpha1.HTTPIngressRuleValue{
						Paths: []netv1alpha1.HTTPIngressPath{{
							RewriteHost: "the-rewrite-host",
							Splits: []netv1alpha1.IngressBackendSplit{{
								Percent: 100,
								AppendHeaders: map[string]string{
									network.OriginalHostHeader: "mapping.com",
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
		dm: v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					"some.annotation":                  "some.value",
					corev1.LastAppliedConfigAnnotation: "blah",
				},
			},
			Spec: v1alpha1.DomainMappingSpec{
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
					"networking.knative.dev/ingress.class": "the-ingress-class",
					"some.annotation":                      "some.value",
				},
			},
			Spec: netv1alpha1.IngressSpec{
				Rules: []netv1alpha1.IngressRule{{
					Hosts:      []string{"mapping.com"},
					Visibility: netv1alpha1.IngressVisibilityExternalIP,
					HTTP: &netv1alpha1.HTTPIngressRuleValue{
						Paths: []netv1alpha1.HTTPIngressPath{{
							RewriteHost: "the-rewrite-host",
							Splits: []netv1alpha1.IngressBackendSplit{{
								Percent: 100,
								AppendHeaders: map[string]string{
									network.OriginalHostHeader: "mapping.com",
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
		dm: v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mapping.com",
				Namespace: "the-namespace",
				Annotations: map[string]string{
					"some.annotation":                  "some.value",
					corev1.LastAppliedConfigAnnotation: "blah",
				},
			},
			Spec: v1alpha1.DomainMappingSpec{
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
					"networking.knative.dev/ingress.class": "the-ingress-class",
					"some.annotation":                      "some.value",
				},
			},
			Spec: netv1alpha1.IngressSpec{
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
									network.OriginalHostHeader: "mapping.com",
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
	}} {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.Labels = kmeta.UnionMaps(tc.dm.Labels, map[string]string{
				serving.DomainMappingLabelKey: tc.dm.Name,
				serving.DomainMappingNamespaceLabelKey: tc.dm.Namespace,
			})
			tc.want.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(&tc.dm)}
			got := *MakeIngress(&tc.dm,
				"the-target-svc", "the-rewrite-host", "the-ingress-class",
				tc.tls, tc.acmeChallenges...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unexpected Ingress (-want, +got):\n%s", diff)
			}
		})
	}

}
