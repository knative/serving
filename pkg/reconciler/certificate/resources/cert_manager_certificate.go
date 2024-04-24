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
	"fmt"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	netapi "knative.dev/networking/pkg/config"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/reconciler/certificate/config"
)

const (
	longest                               = 63
	Prefix                                = "k."
	CreateCertManagerCertificateCondition = "CreateCertManagerCertificate"
	IssuerNotSetCondition                 = "IssuerNotSet"
)

// MakeCertManagerCertificate creates a Cert-Manager `Certificate` for requesting a SSL certificate.
func MakeCertManagerCertificate(cmConfig *config.CertManagerConfig, knCert *v1alpha1.Certificate) (*cmv1.Certificate, *apis.Condition) {
	var commonName string
	var dnsNames []string

	if len(knCert.Spec.DNSNames) > 0 {
		commonName = knCert.Spec.DNSNames[0]
	}

	// Only attempt to do something special if the entry from DNSNames[0] is too big.
	// This is to make the upgrade path easier and reduce churn on certificates.
	// The Route controller adds spec.domain to existing KCerts
	// The KCert controller requests new certs with same domain names, but a different CN if spec.domain is set and the other domain name would be too long
	// cert-manager Certificates are updated only if the existing domain name kept them from being issued.
	if len(commonName) > longest {
		//if we have a domain field, we can attempt to shorten, or check if we are dealing with a domainMapping
		if knCert.Spec.Domain != "" {
			// if the domain and commonName pulled from DNSNames are the same, we are dealing with a domainmapping
			if knCert.Spec.Domain == commonName {
				return nil, &apis.Condition{
					Type:   CreateCertManagerCertificateCondition,
					Status: corev1.ConditionFalse,
					Reason: "CommonName Too Long",
					Message: fmt.Sprintf(
						"error creating cert-manager certificate: CommonName (%s) longer than 63 characters",
						commonName,
					),
				}
			}

			// we have a domain field and are not a domainMapping
			// if the domain is too long, even if we shorten, it will still be too big. We should error in that case
			if len(knCert.Spec.Domain) > (longest - len(Prefix)) {
				return nil, &apis.Condition{
					Type:   CreateCertManagerCertificateCondition,
					Status: corev1.ConditionFalse,
					Reason: "CommonName Too Long",
					Message: fmt.Sprintf(
						"error creating cert-manager certificate: CommonName (%s)(length: %v) too long, prepending short prefix of (%s)(length: %v) will be longer than 64 bytes",
						knCert.Spec.Domain,
						len(knCert.Spec.Domain),
						Prefix,
						len(Prefix),
					),
				}
			}

			// by this point we know:
			// - we have a domain on the kcert
			// - this is not a domain mapping
			// - the first entry on the kcert for dnsNames is too long
			// - the domain is not too long, even with the shortening
			// we can safely shorten the domain and know that it won't be too long

			commonName = Prefix + knCert.Spec.Domain
			dnsNames = append(dnsNames, commonName)

		} else {
			//If there was no domain, we can't shorten anything. We must error.
			return nil, &apis.Condition{
				Type:   CreateCertManagerCertificateCondition,
				Status: corev1.ConditionFalse,
				Reason: "CommonName Too Long",
				Message: fmt.Sprintf(
					"error creating cert-manager certificate: CommonName (%s) too long and field spec.Domain on Kcert is empty, cannot attempt to shorten",
					commonName,
				),
			}
		}
	}

	dnsNames = append(dnsNames, knCert.Spec.DNSNames...)

	// default to CertificateExternalDomain
	certType := netapi.CertificateExternalDomain
	if val, ok := knCert.Labels[networking.CertificateTypeLabelKey]; ok {
		certType = netapi.CertificateType(val)
	}

	var issuerRef cmeta.ObjectReference
	switch certType {
	case netapi.CertificateClusterLocalDomain:
		if cmConfig.ClusterLocalIssuerRef == nil {
			return nil, &apis.Condition{
				Type:    IssuerNotSetCondition,
				Status:  corev1.ConditionFalse,
				Reason:  "clusterLocalIssuerRef not set",
				Message: "error creating cert-manager certificate: clusterLocalIssuerRef was not set in config-certmanager",
			}
		}
		issuerRef = *cmConfig.ClusterLocalIssuerRef

	case netapi.CertificateSystemInternal:
		if cmConfig.SystemInternalIssuerRef == nil {
			return nil, &apis.Condition{
				Type:    IssuerNotSetCondition,
				Status:  corev1.ConditionFalse,
				Reason:  "systemInternalIssuerRef not set",
				Message: "error creating cert-manager certificate: systemInternalIssuerRef was not set in config-certmanager",
			}
		}
		issuerRef = *cmConfig.SystemInternalIssuerRef

	case netapi.CertificateExternalDomain:
		if cmConfig.IssuerRef == nil {
			return nil, &apis.Condition{
				Type:    IssuerNotSetCondition,
				Status:  corev1.ConditionFalse,
				Reason:  "issuerRef not set",
				Message: "error creating cert-manager certificate: issuerRef was not set in config-certmanager",
			}
		}
		issuerRef = *cmConfig.IssuerRef

	default:
		return nil, &apis.Condition{
			Type:    IssuerNotSetCondition,
			Status:  corev1.ConditionFalse,
			Reason:  "certificate type invalid",
			Message: fmt.Sprintf("error creating cert-manager certificate: certificate type %s is invalid", certType),
		}
	}

	cert := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            knCert.Name,
			Namespace:       knCert.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(knCert)},
			Annotations:     knCert.GetAnnotations(),
			Labels:          knCert.GetLabels(),
		},
		Spec: cmv1.CertificateSpec{
			CommonName: commonName,
			SecretName: knCert.Spec.SecretName,
			DNSNames:   dnsNames,
			IssuerRef:  issuerRef,
			SecretTemplate: &cmv1.CertificateSecretTemplate{
				Labels: map[string]string{
					networking.CertificateUIDLabelKey: string(knCert.GetUID()),
				}},
		},
	}
	return cert, nil
}

// GetReadyCondition gets the ready condition of a Cert-Manager `Certificate`.
func GetReadyCondition(cmCert *cmv1.Certificate) *cmv1.CertificateCondition {
	for _, cond := range cmCert.Status.Conditions {
		if cond.Type == cmv1.CertificateConditionReady {
			return &cond
		}
	}
	return nil
}
