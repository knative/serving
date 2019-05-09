/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	. "github.com/knative/pkg/configmap/testing"
	"github.com/knative/pkg/system"
	_ "github.com/knative/pkg/system/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCertManagerConfig(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, CertManagerConfigName)

	if _, err := NewCertManagerConfigFromConfigMap(cm); err != nil {
		t.Errorf("NewCertManagerConfigFromConfigMap(actual) = %v", err)
	}

	if _, err := NewCertManagerConfigFromConfigMap(example); err != nil {
		t.Errorf("NewCertManagerConfigFromConfigMap(actual) = %v", err)
	}
}

func TestIssuerRef(t *testing.T) {
	isserRefCases := []struct {
		name       string
		wantErr    bool
		wantConfig *CertManagerConfig
		config     *corev1.ConfigMap
	}{{
		name:       "invalid format",
		wantErr:    true,
		wantConfig: (*CertManagerConfig)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				issuerRefKey: "wrong format",
			},
		},
	}, {
		name:    "valid IssuerRef",
		wantErr: false,
		wantConfig: &CertManagerConfig{
			SolverConfig: &certmanagerv1alpha1.SolverConfig{},
			IssuerRef: &certmanagerv1alpha1.ObjectReference{
				Name: "letsencrypt-issuer",
				Kind: "ClusterIssuer",
			},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				issuerRefKey: "kind: ClusterIssuer\nname: letsencrypt-issuer",
			},
		},
	}}

	for _, tt := range isserRefCases {
		t.Run(tt.name, func(t *testing.T) {
			actualConfig, err := NewCertManagerConfigFromConfigMap(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewCertManagerConfigFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
			}
			if diff := cmp.Diff(actualConfig, tt.wantConfig); diff != "" {
				t.Fatalf("Want %v, but got %v", tt.wantConfig, actualConfig)
			}
		})
	}
}

func TestSolverConfig(t *testing.T) {
	solverConfigCases := []struct {
		name       string
		wantErr    bool
		wantConfig *CertManagerConfig
		config     *corev1.ConfigMap
	}{{
		name:    "invalid format",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				solverConfigKey: "wrong format",
			},
		},
	}, {
		name:    "valid SolverConfig DNS01",
		wantErr: false,
		wantConfig: &CertManagerConfig{
			SolverConfig: &certmanagerv1alpha1.SolverConfig{
				DNS01: &certmanagerv1alpha1.DNS01SolverConfig{
					Provider: "cloud-dns-provider",
				},
			},
			IssuerRef: &certmanagerv1alpha1.ObjectReference{},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				solverConfigKey: "dns01:\n  provider: cloud-dns-provider",
			},
		},
	}, {
		name:    "valid SolverConfig HTTP01",
		wantErr: false,
		wantConfig: &CertManagerConfig{
			SolverConfig: &certmanagerv1alpha1.SolverConfig{
				HTTP01: &certmanagerv1alpha1.HTTP01SolverConfig{
					Ingress: "test-ingress",
				},
			},
			IssuerRef: &certmanagerv1alpha1.ObjectReference{},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				solverConfigKey: "http01:\n  ingress: test-ingress",
			},
		},
	}}

	for _, tt := range solverConfigCases {
		t.Run(tt.name, func(t *testing.T) {
			actualConfig, err := NewCertManagerConfigFromConfigMap(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewCertManagerConfigFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
			}
			if diff := cmp.Diff(actualConfig, tt.wantConfig); diff != "" {
				t.Fatalf("Want %v, but got %v", tt.wantConfig, actualConfig)
			}
		})
	}
}
