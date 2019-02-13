/*
Copyright 2018 The Knative Authors.

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

package network

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/knative/serving/pkg/reconciler/testing"
)

func TestOurConfig(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, ConfigName)

	if _, err := NewConfigFromConfigMap(cm); err != nil {
		t.Errorf("NewConfigFromConfigMap(actual) = %v", err)
	}
	if _, err := NewConfigFromConfigMap(example); err != nil {
		t.Errorf("NewConfigFromConfigMap(example) = %v", err)
	}
}

func TestConfiguration(t *testing.T) {
	networkConfigTests := []struct {
		name           string
		wantErr        bool
		wantController interface{}
		config         *corev1.ConfigMap
	}{{
		name:    "network configuration with no network input",
		wantErr: false,
		wantController: &Config{
			IstioOutboundIPRanges:      "*",
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
		}}, {
		name:           "network configuration with invalid outbound IP range",
		wantErr:        true,
		wantController: (*Config)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "10.10.10.10/33",
			},
		}}, {
		name:    "network configuration with empty network",
		wantErr: false,
		wantController: &Config{
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "",
			},
		}}, {
		name:           "network configuration with both valid and some invalid range",
		wantErr:        true,
		wantController: (*Config)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "10.10.10.10/12,invalid",
			},
		}}, {
		name:           "network configuration with invalid network range",
		wantErr:        true,
		wantController: (*Config)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "10.10.10.10/12,-1.1.1.1/10",
			},
		}}, {
		name:           "network configuration with invalid network key",
		wantErr:        true,
		wantController: (*Config)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "this is not an IP range",
			},
		}}, {
		name:           "network configuration with invalid network",
		wantErr:        true,
		wantController: (*Config)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "*,*",
			},
		}}, {
		name:           "network configuration with incomplete network array",
		wantErr:        true,
		wantController: (*Config)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "*,",
			},
		}}, {
		name:    "network configuration with invalid network string",
		wantErr: false,
		wantController: &Config{
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: ", ,",
			},
		}}, {
		name:    "network configuration with invalid network string",
		wantErr: false,
		wantController: &Config{
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: ",,",
			},
		}}, {
		name:    "network configuration with invalid network range",
		wantErr: false,
		wantController: &Config{
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: ",",
			},
		}}, {
		name:    "network configuration with valid CIDR network range",
		wantErr: false,
		wantController: &Config{
			IstioOutboundIPRanges:      "10.10.10.0/24",
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "10.10.10.0/24",
			},
		}}, {
		name:    "network configuration with multiple valid network ranges",
		wantErr: false,
		wantController: &Config{
			IstioOutboundIPRanges:      "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
			},
		}}, {
		name:    "network configuration with valid network",
		wantErr: false,
		wantController: &Config{
			IstioOutboundIPRanges:      "*",
			DefaultClusterIngressClass: "istio.ingress.networking.knative.dev",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: "*",
			},
		}}, {
		name:    "network configuration with non-Istio ingress type",
		wantErr: false,
		wantController: &Config{
			IstioOutboundIPRanges:      "*",
			DefaultClusterIngressClass: "foo-ingress",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey:      "*",
				DefaultClusterIngressClassKey: "foo-ingress",
			},
		}},
	}

	for _, tt := range networkConfigTests {
		actualController, err := NewConfigFromConfigMap(tt.config)

		if (err != nil) != tt.wantErr {
			t.Fatalf("Test: %q; NewConfigFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
		}

		if diff := cmp.Diff(actualController, tt.wantController); diff != "" {
			t.Fatalf("Test: %q; want %v, but got %v", tt.name, tt.wantController, actualController)
		}
	}
}
