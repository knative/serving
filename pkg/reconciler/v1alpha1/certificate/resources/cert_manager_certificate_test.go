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
	"testing"

	"github.com/google/go-cmp/cmp"
	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/certificate/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var cert = &v1alpha1.Certificate{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cert",
		Namespace: "test-ns",
	},
	Spec: v1alpha1.CertificateSpec{
		DNSNames:   []string{"host1.example.com", "host2.example.com"},
		SecretName: "secret0",
	},
}

var cmConfig = &config.CertManagerConfig{
	SolverConfig: &certmanagerv1alpha1.SolverConfig{
		DNS01: &certmanagerv1alpha1.DNS01SolverConfig{
			Provider: "cloud-dns-provider",
		},
	},
	IssuerRef: &certmanagerv1alpha1.ObjectReference{
		Kind: "ClusterIssuer",
		Name: "Letsencrypt-issuer",
	},
}

func TestMakeCertManagerCertificate(t *testing.T) {
	tests := []struct {
		name     string
		knCert   *v1alpha1.Certificate
		cmConfig *config.CertManagerConfig
		want     *certmanagerv1alpha1.Certificate
	}{{
		name:     "normal case",
		knCert:   cert,
		cmConfig: cmConfig,
		want: &certmanagerv1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "test-cert",
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(cert)},
			},
			Spec: certmanagerv1alpha1.CertificateSpec{
				SecretName: "secret0",
				DNSNames:   []string{"host1.example.com", "host2.example.com"},
				IssuerRef: certmanagerv1alpha1.ObjectReference{
					Kind: "ClusterIssuer",
					Name: "Letsencrypt-issuer",
				},
				ACME: &certmanagerv1alpha1.ACMECertificateConfig{
					Config: []certmanagerv1alpha1.DomainSolverConfig{
						{
							Domains: []string{"host1.example.com", "host2.example.com"},
							SolverConfig: certmanagerv1alpha1.SolverConfig{
								DNS01: &certmanagerv1alpha1.DNS01SolverConfig{
									Provider: "cloud-dns-provider",
								},
							},
						},
					},
				},
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeCertManagerCertificate(test.cmConfig, test.knCert)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeCertManagerCertificate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestIsCertManagerCertificateReady(t *testing.T) {
	tests := []struct {
		name          string
		cmCertificate *certmanagerv1alpha1.Certificate
		want          bool
	}{{
		name:          "ready",
		cmCertificate: makeTestCertificate(true),
		want:          true,
	}, {
		name:          "not ready",
		cmCertificate: makeTestCertificate(false),
		want:          false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsCertManagerCertificateReady(test.cmCertificate)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("IsCertManagerCertificateReady (-want, +got) = %v", diff)
			}
		})
	}
}

func makeTestCertificate(isReady bool) *certmanagerv1alpha1.Certificate {
	cert := &certmanagerv1alpha1.Certificate{}
	var conditionStatus certmanagerv1alpha1.ConditionStatus
	if isReady {
		conditionStatus = certmanagerv1alpha1.ConditionTrue
	} else {
		conditionStatus = certmanagerv1alpha1.ConditionFalse
	}
	cert.UpdateStatusCondition(certmanagerv1alpha1.CertificateConditionReady, conditionStatus, "", "", false)
	return cert
}
