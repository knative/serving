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
	"strings"
	"testing"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	netapi "knative.dev/networking/pkg/config"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/reconciler/certificate/config"
)

const (
	servingRouteLabelKey     = "serving.knative.dev/route"
	servingCreatorAnnotation = "serving.knative.dev/creator"
	servingUpdaterAnnotation = "serving.knative.dev/lastModifier"
)

var cert = &v1alpha1.Certificate{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cert",
		Namespace: "test-ns",
		UID:       "22b3de9e-076e-4e5d-a55d-aff10002527f",
		Labels: map[string]string{
			servingRouteLabelKey:               "test-route",
			networking.CertificateTypeLabelKey: string(netapi.CertificateExternalDomain),
		},
		Annotations: map[string]string{
			servingCreatorAnnotation: "someone",
			servingUpdaterAnnotation: "someone",
		},
	},
	Spec: v1alpha1.CertificateSpec{
		DNSNames:   []string{"host1.example.com", "host2.example.com"},
		Domain:     "example.com",
		SecretName: "secret0",
	},
}

var localCert = &v1alpha1.Certificate{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-local-cert",
		Namespace: "test-ns",
		UID:       "22b3de9e-076e-4e5d-a55d-aff10002527i",
		Labels: map[string]string{
			servingRouteLabelKey:               "test-route-local",
			networking.CertificateTypeLabelKey: string(netapi.CertificateClusterLocalDomain),
		},
		Annotations: map[string]string{
			servingCreatorAnnotation: "someone",
			servingUpdaterAnnotation: "someone",
		},
	},
	Spec: v1alpha1.CertificateSpec{
		DNSNames:   []string{"host1.ns", "host1.ns.svc", "host1.ns.svc.cluster.local"},
		Domain:     "cluster.local",
		SecretName: "secret0",
	},
}

var systemInternalCert = &v1alpha1.Certificate{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-system-internal-cert",
		Namespace: "test-ns",
		UID:       "22b3de9e-076e-4e5d-a55d-aff10002527i",
		Labels: map[string]string{
			servingRouteLabelKey:               "test-route-system-internal",
			networking.CertificateTypeLabelKey: string(netapi.CertificateSystemInternal),
		},
		Annotations: map[string]string{
			servingCreatorAnnotation: "someone",
			servingUpdaterAnnotation: "someone",
		},
	},
	Spec: v1alpha1.CertificateSpec{
		DNSNames:   []string{"host1.ns", "host1.ns.svc", "host1.ns.svc.cluster.local"},
		Domain:     "cluster.local",
		SecretName: "secret0",
	},
}

var (
	longHost         = "somebighost12345678910.somebignamespacename12345678910"
	domain           = "some.domain.test"
	longHostDNSNames = []string{longHost + "." + domain}
	certWithLongHost = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cert",
			Namespace: "test-ns",
			UID:       "22b3de9e-076e-4e5d-a55d-aff10002527f",
			Labels: map[string]string{
				servingRouteLabelKey:               "test-route",
				networking.CertificateTypeLabelKey: string(netapi.CertificateExternalDomain),
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   longHostDNSNames,
			Domain:     domain,
			SecretName: "secret0",
		},
	}

	longDomain         = fmt.Sprintf("%s.%s", strings.Repeat("a", 60), "com")
	longDomainDNSNames = []string{"host1." + longDomain, "host2." + longDomain}
	certWithLongDomain = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cert",
			Namespace: "test-ns",
			UID:       "22b3de9e-076e-4e5d-a55d-aff10002527f",
			Labels: map[string]string{
				servingRouteLabelKey:               "test-route",
				networking.CertificateTypeLabelKey: string(netapi.CertificateExternalDomain),
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   longDomainDNSNames,
			Domain:     longDomain,
			SecretName: "secret0",
		},
	}

	cmConfig = &config.CertManagerConfig{
		IssuerRef: &cmmeta.ObjectReference{
			Kind: "ClusterIssuer",
			Name: "Letsencrypt-issuer",
		},
		ClusterLocalIssuerRef: &cmmeta.ObjectReference{
			Kind: "ClusterIssuer",
			Name: "knative-selfsigned-issuer",
		},
		SystemInternalIssuerRef: &cmmeta.ObjectReference{
			Kind: "ClusterIssuer",
			Name: "knative-selfsigned-issuer",
		},
	}
)

func TestMakeCertManagerExternalCertificate(t *testing.T) {
	want := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cert",
			Namespace:       "test-ns",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(cert)},
			Labels: map[string]string{
				servingRouteLabelKey:               "test-route",
				networking.CertificateTypeLabelKey: string(netapi.CertificateExternalDomain),
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: "secret0",
			CommonName: "host1.example.com",
			DNSNames:   []string{"host1.example.com", "host2.example.com"},
			IssuerRef: cmmeta.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "Letsencrypt-issuer",
			},
			SecretTemplate: &cmv1.CertificateSecretTemplate{
				Labels: map[string]string{networking.CertificateUIDLabelKey: "22b3de9e-076e-4e5d-a55d-aff10002527f"},
			},
		},
	}
	got, err := MakeCertManagerCertificate(cmConfig, cert)
	if err != nil {
		t.Errorf("MakeCertManagerCertificate Error: %s", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerLocalCertificate(t *testing.T) {
	want := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-local-cert",
			Namespace:       "test-ns",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(localCert)},
			Labels: map[string]string{
				servingRouteLabelKey:               "test-route-local",
				networking.CertificateTypeLabelKey: string(netapi.CertificateClusterLocalDomain),
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: "secret0",
			CommonName: "host1.ns",
			DNSNames:   []string{"host1.ns", "host1.ns.svc", "host1.ns.svc.cluster.local"},
			IssuerRef: cmmeta.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "knative-selfsigned-issuer",
			},
			SecretTemplate: &cmv1.CertificateSecretTemplate{
				Labels: map[string]string{networking.CertificateUIDLabelKey: "22b3de9e-076e-4e5d-a55d-aff10002527i"},
			},
		},
	}
	got, err := MakeCertManagerCertificate(cmConfig, localCert)
	if err != nil {
		t.Errorf("MakeCertManagerCertificate Error: %s", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerSystemInternalCertificate(t *testing.T) {
	want := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-system-internal-cert",
			Namespace:       "test-ns",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(systemInternalCert)},
			Labels: map[string]string{
				servingRouteLabelKey:               "test-route-system-internal",
				networking.CertificateTypeLabelKey: string(netapi.CertificateSystemInternal),
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: "secret0",
			CommonName: "host1.ns",
			DNSNames:   []string{"host1.ns", "host1.ns.svc", "host1.ns.svc.cluster.local"},
			IssuerRef: cmmeta.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "knative-selfsigned-issuer",
			},
			SecretTemplate: &cmv1.CertificateSecretTemplate{
				Labels: map[string]string{networking.CertificateUIDLabelKey: "22b3de9e-076e-4e5d-a55d-aff10002527i"},
			},
		},
	}
	got, err := MakeCertManagerCertificate(cmConfig, systemInternalCert)
	if err != nil {
		t.Errorf("MakeCertManagerCertificate Error: %s", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerCertificateLongCommonName(t *testing.T) {
	want := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cert",
			Namespace:       "test-ns",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(cert)},
			Labels: map[string]string{
				servingRouteLabelKey:               "test-route",
				networking.CertificateTypeLabelKey: string(netapi.CertificateExternalDomain),
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: "secret0",
			CommonName: "k.some.domain.test",
			DNSNames:   append([]string{"k.some.domain.test"}, longHostDNSNames...),
			IssuerRef: cmmeta.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "Letsencrypt-issuer",
			},
			SecretTemplate: &cmv1.CertificateSecretTemplate{
				Labels: map[string]string{networking.CertificateUIDLabelKey: "22b3de9e-076e-4e5d-a55d-aff10002527f"},
			},
		},
	}
	got, err := MakeCertManagerCertificate(cmConfig, certWithLongHost)
	if err != nil {
		t.Errorf("MakeCertManagerCertificate Error: %s", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerCertificateDomainMappingIsTooLong(t *testing.T) {
	wantError := fmt.Errorf("error creating cert-manager certificate: CommonName (this.is.aaaaaaaaaaaaaaa.reallyreallyreallyreallyreallylong.domainmapping) longer than 63 characters")
	cert, gotError := MakeCertManagerCertificate(cmConfig, &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cert-from-domain-mapping",
			Namespace: "test-ns",
			UID:       "22b3de9e-076e-4e5d-a55d-aff10002527f",
			Labels: map[string]string{
				servingRouteLabelKey: "test-route",
			},
			Annotations: map[string]string{
				servingCreatorAnnotation: "someone",
				servingUpdaterAnnotation: "someone",
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{"this.is.aaaaaaaaaaaaaaa.reallyreallyreallyreallyreallylong.domainmapping"},
			Domain:     "this.is.aaaaaaaaaaaaaaa.reallyreallyreallyreallyreallylong.domainmapping",
			SecretName: "secret0",
		},
	})

	if cert != nil {
		t.Errorf("Expected no cert, got: %s", cmp.Diff(nil, cert))
	}

	if diff := cmp.Diff(wantError.Error(), gotError.Message); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerCertificateDomainIsTooLong(t *testing.T) {
	wantError := fmt.Errorf("error creating cert-manager certificate: CommonName (aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com)(length: 64) too long, prepending short prefix of (k.)(length: 2) will be longer than 64 bytes")
	cert, gotError := MakeCertManagerCertificate(cmConfig, certWithLongDomain)

	if cert != nil {
		t.Errorf("Expected no cert, got: %s", cmp.Diff(nil, cert))
	}

	if diff := cmp.Diff(wantError.Error(), gotError.Message); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerCertificateIssuerNotSet(t *testing.T) {
	wantError := fmt.Errorf("error creating cert-manager certificate: issuerRef was not set in config-certmanager")

	cmConfigNoIssuer := cmConfig.DeepCopy()
	cmConfigNoIssuer.IssuerRef = nil

	cert, gotError := MakeCertManagerCertificate(cmConfigNoIssuer, cert)

	if cert != nil {
		t.Errorf("Expected no cert, got: %s", cmp.Diff(nil, cert))
	}

	if diff := cmp.Diff(wantError.Error(), gotError.Message); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerCertificateLocalIssuerNotSet(t *testing.T) {
	wantError := fmt.Errorf("error creating cert-manager certificate: clusterLocalIssuerRef was not set in config-certmanager")

	cmConfigNoIssuer := cmConfig.DeepCopy()
	cmConfigNoIssuer.ClusterLocalIssuerRef = nil

	cert, gotError := MakeCertManagerCertificate(cmConfigNoIssuer, localCert)

	if cert != nil {
		t.Errorf("Expected no cert, got: %s", cmp.Diff(nil, cert))
	}

	if diff := cmp.Diff(wantError.Error(), gotError.Message); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestMakeCertManagerCertificateSystemInternalIssuerNotSet(t *testing.T) {
	wantError := fmt.Errorf("error creating cert-manager certificate: systemInternalIssuerRef was not set in config-certmanager")

	cmConfigNoIssuer := cmConfig.DeepCopy()
	cmConfigNoIssuer.SystemInternalIssuerRef = nil

	cert, gotError := MakeCertManagerCertificate(cmConfigNoIssuer, systemInternalCert)

	if cert != nil {
		t.Errorf("Expected no cert, got: %s", cmp.Diff(nil, cert))
	}

	if diff := cmp.Diff(wantError.Error(), gotError.Message); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestGetReadyCondition(t *testing.T) {
	tests := []struct {
		name          string
		cmCertificate *cmv1.Certificate
		want          *cmv1.CertificateCondition
	}{{
		name:          "ready",
		cmCertificate: makeTestCertificate(cmmeta.ConditionTrue, cmv1.CertificateConditionReady, "ready", "ready"),
		want: &cmv1.CertificateCondition{
			Type:    cmv1.CertificateConditionReady,
			Status:  cmmeta.ConditionTrue,
			Reason:  "ready",
			Message: "ready",
		}}, {
		name:          "not ready",
		cmCertificate: makeTestCertificate(cmmeta.ConditionFalse, cmv1.CertificateConditionReady, "not ready", "not ready"),
		want: &cmv1.CertificateCondition{
			Type:    cmv1.CertificateConditionReady,
			Status:  cmmeta.ConditionFalse,
			Reason:  "not ready",
			Message: "not ready",
		}}, {
		name:          "unknow",
		cmCertificate: makeTestCertificate(cmmeta.ConditionUnknown, cmv1.CertificateConditionReady, "unknown", "unknown"),
		want: &cmv1.CertificateCondition{
			Type:    cmv1.CertificateConditionReady,
			Status:  cmmeta.ConditionUnknown,
			Reason:  "unknown",
			Message: "unknown",
		}}, {
		name:          "condition not ready",
		cmCertificate: makeTestCertificate(cmmeta.ConditionTrue, cmv1.CertificateConditionIssuing, "Renewing", "Renewing certificate"),
		want:          nil,
	},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GetReadyCondition(test.cmCertificate)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("GetReadyCondition (-want, +got) = %s", diff)
			}
		})
	}
}

func makeTestCertificate(condStatus cmmeta.ConditionStatus, condType cmv1.CertificateConditionType, reason, message string) *cmv1.Certificate {
	cert := &cmv1.Certificate{
		Status: cmv1.CertificateStatus{
			Conditions: []cmv1.CertificateCondition{{
				Type:    condType,
				Status:  condStatus,
				Reason:  reason,
				Message: message,
			}},
		},
	}
	return cert
}
