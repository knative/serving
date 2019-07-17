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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestIstio(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, IstioConfigName)

	if _, err := NewIstioFromConfigMap(cm); err != nil {
		t.Errorf("NewIstioFromConfigMap(actual) = %v", err)
	}

	if _, err := NewIstioFromConfigMap(example); err != nil {
		t.Errorf("NewIstioFromConfigMap(example) = %v", err)
	}
}

func TestGatewayConfiguration(t *testing.T) {
	gatewayConfigTests := []struct {
		name      string
		wantErr   bool
		wantIstio interface{}
		config    *corev1.ConfigMap
	}{{
		name: "gateway configuration with no network input",
		wantIstio: &Istio{
			IngressGateways: []Gateway{defaultGateway},
			LocalGateways:   []Gateway{defaultLocalGateway},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
		},
	}, {
		name:      "gateway configuration with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.invalid": "_invalid",
			},
		},
	}, {
		name:    "gateway configuration with valid url",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				GatewayName: "knative-ingress-freeway",
				ServiceURL:  "istio-ingressfreeway.istio-system.svc.cluster.local",
			}},
			LocalGateways: []Gateway{defaultLocalGateway},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.knative-ingress-freeway": "istio-ingressfreeway.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:    "local gateway configuration with valid url",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{defaultGateway},
			LocalGateways: []Gateway{{
				GatewayName: "knative-ingress-backroad",
				ServiceURL:  "istio-ingressbackroad.istio-system.svc.cluster.local",
			}},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.knative-ingress-backroad": "istio-ingressbackroad.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:    "local gateway configuration with mesh",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{defaultGateway},
			LocalGateways:   []Gateway{},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.mesh": "mesh",
			},
		},
	}}

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

func TestReconcileGatewayConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		want   bool
		config *corev1.ConfigMap
	}{{
		name: "enable ReconcileExternalGateway",
		want: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"reconcileExternalGateway": "true",
			},
		},
	}, {
		name: "disable ReconcileExternalGateway",
		want: false,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"reconcileExternalGateway": "false",
			},
		},
	}, {
		name: "disable by default",
		want: false,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{},
		},
	}}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			istio, err := NewIstioFromConfigMap(tt.config)
			if err != nil {
				t.Fatalf("Test: %q; NewIstioFromConfigMap() error = %v", tt.name, err)
			}
			if tt.want != istio.ReconcileExternalGateway {
				t.Fatalf("Unexpected result (-want %t, +got %t)", tt.want, istio.ReconcileExternalGateway)
			}
		})
	}
}
