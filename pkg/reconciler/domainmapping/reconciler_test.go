/*
Copyright 2021 The Knative Authors

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

package domainmapping

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

func TestAutoTLSEnabled(t *testing.T) {
	dm := domainMapping("test-ns", "test-route")

	for _, tc := range []struct {
		name                  string
		configAutoTLSEnabled  bool
		tlsDisabledAnnotation string
		wantAutoTLSEnabled    bool
	}{{
		name:                 "AutoTLS enabled by config, not disabled by annotation",
		configAutoTLSEnabled: true,
		wantAutoTLSEnabled:   true,
	}, {
		name:                  "AutoTLS enabled by config, disabled by annotation",
		configAutoTLSEnabled:  true,
		tlsDisabledAnnotation: "true",
		wantAutoTLSEnabled:    false,
	}, {
		name:                 "AutoTLS disabled by config, not disabled by annotation",
		configAutoTLSEnabled: false,
		wantAutoTLSEnabled:   false,
	}, {
		name:                  "AutoTLS disabled by config, disabled by annotation",
		configAutoTLSEnabled:  false,
		tlsDisabledAnnotation: "true",
		wantAutoTLSEnabled:    false,
	}, {
		name:                  "AutoTLS enabled by config, invalid annotation",
		configAutoTLSEnabled:  true,
		tlsDisabledAnnotation: "foo",
		wantAutoTLSEnabled:    true,
	}, {
		name:                  "AutoTLS disabled by config, invalid annotation",
		configAutoTLSEnabled:  false,
		tlsDisabledAnnotation: "foo",
		wantAutoTLSEnabled:    false,
	}, {
		name:                 "AutoTLS disabled by config nil annotations",
		configAutoTLSEnabled: false,
		wantAutoTLSEnabled:   false,
	}, {
		name:                 "AutoTLS enabled by config, nil annotations",
		configAutoTLSEnabled: true,
		wantAutoTLSEnabled:   true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := config.ToContext(context.Background(), &config.Config{
				Network: &network.Config{
					AutoTLS: tc.configAutoTLSEnabled,
				},
			})
			if tc.tlsDisabledAnnotation != "" {
				dm.Annotations = map[string]string{
					networking.DisableAutoTLSAnnotationKey: tc.tlsDisabledAnnotation,
				}
			}
			if got := autoTLSEnabled(ctx, dm); got != tc.wantAutoTLSEnabled {
				t.Errorf("autoTLSEnabled = %t, want %t", got, tc.wantAutoTLSEnabled)
			}
		})
	}
}

func TestTLSCertificateProvided(t *testing.T) {
	dm := domainMapping("test-ns", "test-domain")

	reconciler := Reconciler{}

	for _, tc := range []struct {
		name                     string
		autoTLS                  bool
		secretSpec               *v1alpha1.SecretTLS
		wantCerficateProvisioned corev1.ConditionStatus
		wantIngressTLS           []netv1alpha1.IngressTLS
		wantURLScheme            string
	}{{
		name:                     "Secret Spec missing",
		secretSpec:               nil,
		autoTLS:                  false,
		wantCerficateProvisioned: "True",
	}, {
		name:    "Secret without namespace uses DomainMapping's namespace",
		autoTLS: true,
		secretSpec: &v1alpha1.SecretTLS{
			SecretName: "tls-secret",
		},
		wantCerficateProvisioned: "True",
		wantIngressTLS: []netv1alpha1.IngressTLS{{
			Hosts:           []string{dm.Name},
			SecretName:      "tls-secret",
			SecretNamespace: dm.Namespace,
		}},
		wantURLScheme: "https",
	}, {
		name:    "Secret with namespace",
		autoTLS: true,
		secretSpec: &v1alpha1.SecretTLS{
			SecretName:      "tls-secret",
			SecretNamespace: "secret-ns",
		},
		wantCerficateProvisioned: "True",
		wantIngressTLS: []netv1alpha1.IngressTLS{{
			Hosts:           []string{dm.Name},
			SecretName:      "tls-secret",
			SecretNamespace: "secret-ns",
		}},
		wantURLScheme: "https",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			dm.Spec.TLS = tc.secretSpec
			dm.Status = v1alpha1.DomainMappingStatus{
				URL: &apis.URL{},
			}
			ctx := config.ToContext(context.Background(), &config.Config{
				Network: &network.Config{
					AutoTLS: tc.autoTLS,
				},
			})
			gotIngressTLS, _, err := reconciler.tls(ctx, dm)
			if err != nil {
				t.Fatalf("Unexpected error calling tls on DomainMapping reconciler: %v", err)
			}
			if !cmp.Equal(gotIngressTLS, tc.wantIngressTLS) {
				t.Fatal("Expected IngressTLS didn't match (-want, +got)", cmp.Diff(tc.wantIngressTLS, gotIngressTLS))
			}
			if dm.Status.URL.Scheme != tc.wantURLScheme {
				t.Fatalf("Expected DomainMapping URL.Scheme to be \"%s\" but got \"%s\"", tc.wantURLScheme, dm.Status.URL.Scheme)
			}
			certificateProvisionedCondition := dm.Status.GetCondition(v1alpha1.DomainMappingConditionCertificateProvisioned)
			if certificateProvisionedCondition.Status != tc.wantCerficateProvisioned {
				t.Fatalf("Expected CertificateProvisioned condition to be \"%s\" but got \"%s\", got reason %s", tc.wantCerficateProvisioned, certificateProvisionedCondition.Status, certificateProvisionedCondition.Reason)
			}
		})
	}
}
