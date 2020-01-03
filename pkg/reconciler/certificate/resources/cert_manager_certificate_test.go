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
	cmv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/reconciler/certificate/config"
)

var cert = &v1alpha1.Certificate{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cert",
		Namespace: "test-ns",
		Labels: map[string]string{
			serving.RouteLabelKey: "test-route",
		},
		Annotations: map[string]string{
			serving.CreatorAnnotation: "someone",
			serving.UpdaterAnnotation: "someone",
		},
	},
	Spec: v1alpha1.CertificateSpec{
		DNSNames:   []string{"host1.example.com", "host2.example.com"},
		SecretName: "secret0",
	},
}

var cmConfig = &config.CertManagerConfig{
	IssuerRef: &cmmeta.ObjectReference{
		Kind: "ClusterIssuer",
		Name: "Letsencrypt-issuer",
	},
}

func TestMakeCertManagerCertificate(t *testing.T) {
	want := &cmv1alpha2.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cert",
			Namespace:       "test-ns",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(cert)},
			Labels: map[string]string{
				serving.RouteLabelKey: "test-route",
			},
			Annotations: map[string]string{
				serving.CreatorAnnotation: "someone",
				serving.UpdaterAnnotation: "someone",
			},
		},
		Spec: cmv1alpha2.CertificateSpec{
			SecretName: "secret0",
			DNSNames:   []string{"host1.example.com", "host2.example.com"},
			IssuerRef: cmmeta.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "Letsencrypt-issuer",
			},
		},
	}
	got := MakeCertManagerCertificate(cmConfig, cert)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertManagerCertificate (-want, +got) = %s", diff)
	}
}

func TestGetReadyCondition(t *testing.T) {
	tests := []struct {
		name          string
		cmCertificate *cmv1alpha2.Certificate
		want          *cmv1alpha2.CertificateCondition
	}{{
		name:          "ready",
		cmCertificate: makeTestCertificate(cmmeta.ConditionTrue, "ready", "ready"),
		want: &cmv1alpha2.CertificateCondition{
			Type:    cmv1alpha2.CertificateConditionReady,
			Status:  cmmeta.ConditionTrue,
			Reason:  "ready",
			Message: "ready",
		}}, {
		name:          "not ready",
		cmCertificate: makeTestCertificate(cmmeta.ConditionFalse, "not ready", "not ready"),
		want: &cmv1alpha2.CertificateCondition{
			Type:    cmv1alpha2.CertificateConditionReady,
			Status:  cmmeta.ConditionFalse,
			Reason:  "not ready",
			Message: "not ready",
		}}, {
		name:          "unknow",
		cmCertificate: makeTestCertificate(cmmeta.ConditionUnknown, "unknown", "unknown"),
		want: &cmv1alpha2.CertificateCondition{
			Type:    cmv1alpha2.CertificateConditionReady,
			Status:  cmmeta.ConditionUnknown,
			Reason:  "unknown",
			Message: "unknown",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GetReadyCondition(test.cmCertificate)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("GetReadyCondition (-want, +got) = %s", diff)
			}
		})
	}
}

func makeTestCertificate(cond cmmeta.ConditionStatus, reason, message string) *cmv1alpha2.Certificate {
	cert := &cmv1alpha2.Certificate{
		Status: cmv1alpha2.CertificateStatus{
			Conditions: []cmv1alpha2.CertificateCondition{{
				Type:    cmv1alpha2.CertificateConditionReady,
				Status:  cond,
				Reason:  reason,
				Message: message,
			}},
		},
	}
	return cert
}
