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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	servingv1alpha1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
)

// MakeIngress creates an Ingress object for a DomainMapping.  The Ingress is
// always created in the same namespace as the DomainMapping, and the ingress
// backend is always in the same namespace also (as this is required by
// KIngress).  The created ingress will contain a RewriteHost rule to cause the
// given hostName to be used as the host.
func MakeIngress(dm *servingv1alpha1.DomainMapping, backendServiceName, hostName, ingressClass string) *netv1alpha1.Ingress {
	return &netv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmeta.ChildName(dm.GetName(), ""),
			Namespace: dm.Namespace,
			Annotations: kmeta.FilterMap(kmeta.UnionMaps(map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
			}, dm.GetAnnotations()), func(key string) bool {
				return key == corev1.LastAppliedConfigAnnotation
			}),
			Labels: kmeta.UnionMaps(dm.Labels, map[string]string{
				serving.DomainMappingLabelKey:          dm.Name,
				serving.DomainMappingNamespaceLabelKey: dm.Namespace,
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(dm)},
		},
		Spec: netv1alpha1.IngressSpec{
			Rules: []netv1alpha1.IngressRule{{
				Hosts:      []string{dm.Name},
				Visibility: netv1alpha1.IngressVisibilityExternalIP,
				HTTP: &netv1alpha1.HTTPIngressRuleValue{
					Paths: []netv1alpha1.HTTPIngressPath{{
						RewriteHost: hostName,
						Splits: []netv1alpha1.IngressBackendSplit{{
							Percent: 100,
							AppendHeaders: map[string]string{
								network.OriginalHostHeader: dm.Name,
							},
							IngressBackend: netv1alpha1.IngressBackend{
								ServiceNamespace: dm.Namespace,
								ServiceName:      backendServiceName,
								ServicePort:      intstr.FromInt(80),
							},
						}},
					}},
				},
			}},
		},
	}
}
