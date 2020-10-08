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
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

func TestMakeIngress(t *testing.T) {
	dm := &v1alpha1.DomainMapping{
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
	}

	got := MakeIngress(dm, "the-ingress-class")

	want := &netv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mapping.com",
			Namespace: "the-namespace",
			Annotations: map[string]string{
				"networking.knative.dev/ingress.class": "the-ingress-class",
				"some.annotation":                      "some.value",
			},
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(dm)},
		},
		Spec: netv1alpha1.IngressSpec{
			Rules: []netv1alpha1.IngressRule{{
				Hosts:      []string{"mapping.com"},
				Visibility: netv1alpha1.IngressVisibilityExternalIP,
				HTTP: &netv1alpha1.HTTPIngressRuleValue{
					Paths: []netv1alpha1.HTTPIngressPath{{
						RewriteHost: "the-name.the-namespace.svc.cluster.local",
						Splits: []netv1alpha1.IngressBackendSplit{{
							Percent: 100,
							IngressBackend: netv1alpha1.IngressBackend{
								ServiceName:      "the-name",
								ServiceNamespace: "the-namespace",
								ServicePort:      intstr.FromInt(80),
							},
						}},
					}},
				},
			}},
		},
	}

	if !cmp.Equal(want, got) {
		t.Error("Unexpected Ingress (-want, +got):\n", cmp.Diff(want, got))
	}
}
