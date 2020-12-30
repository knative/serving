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
	"knative.dev/networking/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

// MakeCertificate creates a Certificate for the DomainMapping.
func MakeCertificate(dm *v1alpha1.DomainMapping, certClass string) *networkingv1alpha1.Certificate {
	certName := kmeta.ChildName(dm.GetName(), "")
	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: dm.Namespace,
			Annotations: kmeta.FilterMap(kmeta.UnionMaps(map[string]string{
				networking.CertificateClassAnnotationKey: certClass,
			}, dm.Annotations), func(key string) bool {
				return key == corev1.LastAppliedConfigAnnotation
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(dm)},
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames: []string{
				dm.GetName(),
			},
			SecretName: certName,
		},
	}
}
