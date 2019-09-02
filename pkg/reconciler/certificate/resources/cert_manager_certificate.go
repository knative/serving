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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler/certificate/config"
)

// MakeCertManagerCertificate creates a Cert-Manager `Certificate` for requesting a SSL certificate.
func MakeCertManagerCertificate(cmConfig *config.CertManagerConfig, knCert *v1alpha1.Certificate) *certmanagerv1alpha1.Certificate {
	cert := &certmanagerv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            knCert.Name,
			Namespace:       knCert.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(knCert)},
			Annotations:     knCert.GetAnnotations(),
			Labels:          knCert.GetLabels(),
		},
		Spec: certmanagerv1alpha1.CertificateSpec{
			SecretName: knCert.Spec.SecretName,
			DNSNames:   knCert.Spec.DNSNames,
			IssuerRef:  *cmConfig.IssuerRef,
		},
	}

	switch cmConfig.IssuerKind {
	case "acme":
		cert.Spec.ACME = &certmanagerv1alpha1.ACMECertificateConfig{
			Config: []certmanagerv1alpha1.DomainSolverConfig{{
				Domains:      knCert.Spec.DNSNames,
				SolverConfig: *cmConfig.SolverConfig,
			}},
		}
	case "ca":
		// ca does not need any specific config.
	}
	return cert
}

// GetReadyCondition gets the ready condition of a Cert-Manager `Certificate`.
func GetReadyCondition(cmCert *certmanagerv1alpha1.Certificate) *certmanagerv1alpha1.CertificateCondition {
	for _, cond := range cmCert.Status.Conditions {
		if cond.Type == certmanagerv1alpha1.CertificateConditionReady {
			return &cond
		}
	}
	return nil
}
