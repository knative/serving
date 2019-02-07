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
	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/certificate/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeCertManagerCertificate creates a Cert-Manager `Certificate` for requesting a SSL certificate.
func MakeCertManagerCertificate(cmConfig *config.CertManagerConfig, knCert *v1alpha1.Certificate) *certmanagerv1alpha1.Certificate {
	return &certmanagerv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            knCert.Name,
			Namespace:       knCert.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(knCert)},
		},
		Spec: certmanagerv1alpha1.CertificateSpec{
			SecretName: knCert.Spec.SecretName,
			DNSNames:   knCert.Spec.DNSNames,
			IssuerRef:  *cmConfig.IssuerRef,
			ACME: &certmanagerv1alpha1.ACMECertificateConfig{
				Config: []certmanagerv1alpha1.DomainSolverConfig{{
					Domains:      knCert.Spec.DNSNames,
					SolverConfig: *cmConfig.SolverConfig,
				}},
			},
		},
	}
}

// IsCertManagerCertificateReady returns if a Cert-Manager `Certificate` is ready for use.
func IsCertManagerCertificateReady(cmCert *certmanagerv1alpha1.Certificate) bool {
	return cmCert.HasCondition(certmanagerv1alpha1.CertificateCondition{
		Type:   certmanagerv1alpha1.CertificateConditionReady,
		Status: certmanagerv1alpha1.ConditionTrue,
	})
}
