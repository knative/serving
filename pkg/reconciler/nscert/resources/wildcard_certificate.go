/*
Copyright 2019 The Knative Authors

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
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler/nscert/resources/names"
)

// MakeWildcardCertificate creates a Knative certificate
func MakeWildcardCertificate(namespace *corev1.Namespace, dnsName, domain string) *v1alpha1.Certificate {
	return &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.WildcardCertificate(dnsName),
			Namespace:       namespace.Name,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(namespace, corev1.SchemeGroupVersion.WithKind("Namespace"))},
			Labels: map[string]string{
				networking.WildcardCertDomainLabelKey: domain,
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{dnsName},
			SecretName: names.WildcardCertificate(dnsName),
		},
	}
}
