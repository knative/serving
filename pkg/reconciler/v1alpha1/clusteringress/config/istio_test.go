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

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/knative/serving/pkg/reconciler/testing"
)

func TestIstio(t *testing.T) {
	cm := ConfigMapFromTestFile(t, IstioConfigName)

	if _, err := NewIstioFromConfigMap(cm); err != nil {
		t.Errorf("NewIstioFromConfigMap() = %v", err)
	}
}

func TestGatewayConfiguration(t *testing.T) {
	gatewayConfigTests := []struct {
		name      string
		wantErr   bool
		wantIstio interface{}
		config    *corev1.ConfigMap
	}{{
		name:      "gateway configuration with no network input",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      IstioConfigName,
			},
		}}, {
		name:      "gateway configuration with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.invalid": "_invalid",
			},
		}}, {
		name:    "gateway configuration with valid url",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				GatewayName: "knative-ingress-gateway",
				ServiceURL:  "istio-ingressgateway.istio-system.svc.cluster.local",
			}},
			LocalGateways: []Gateway{},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.knative-ingress-gateway": "istio-ingressgateway.istio-system.svc.cluster.local",
			},
		}},
	}

	for _, tt := range gatewayConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			actualIstio, err := NewIstioFromConfigMap(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewIstioFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
			}

			if diff := cmp.Diff(actualIstio, tt.wantIstio); diff != "" {
				t.Fatalf("Want %v, but got %v", tt.wantIstio, actualIstio)
			}
		})
	}
}
