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

package config

import (
	"testing"

	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	configmaptesting "knative.dev/pkg/configmap/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
)

func TestCertManagerConfig(t *testing.T) {
	cm, example := configmaptesting.ConfigMapsFromTestFile(t, CertManagerConfigName)

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
				issuerRefKey:             "wrong format",
				clusterLocalIssuerRefKey: "wrong format",
				systemInternalIssuerRef:  "wrong format",
			},
		},
	}, {
		name:    "valid IssuerRef",
		wantErr: false,
		wantConfig: &CertManagerConfig{
			IssuerRef: &cmmeta.ObjectReference{
				Name: "letsencrypt-issuer",
				Kind: "ClusterIssuer",
			},
			ClusterLocalIssuerRef:   knativeSelfSignedIssuer,
			SystemInternalIssuerRef: knativeSelfSignedIssuer,
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
	}, {
		name:    "valid ClusterLocalIssuerRef",
		wantErr: false,
		wantConfig: &CertManagerConfig{
			IssuerRef: knativeSelfSignedIssuer,
			ClusterLocalIssuerRef: &cmmeta.ObjectReference{
				Name: "cluster-local-issuer",
				Kind: "ClusterIssuer",
			},
			SystemInternalIssuerRef: knativeSelfSignedIssuer,
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				clusterLocalIssuerRefKey: "kind: ClusterIssuer\nname: cluster-local-issuer",
			},
		},
	}, {
		name:    "valid SystemInternalIssuerRef",
		wantErr: false,
		wantConfig: &CertManagerConfig{
			IssuerRef:             knativeSelfSignedIssuer,
			ClusterLocalIssuerRef: knativeSelfSignedIssuer,
			SystemInternalIssuerRef: &cmmeta.ObjectReference{
				Name: "system-internal-issuer",
				Kind: "ClusterIssuer",
			},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      CertManagerConfigName,
			},
			Data: map[string]string{
				clusterLocalIssuerRefKey: "kind: ClusterIssuer\nname: system-internal-issuer",
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
