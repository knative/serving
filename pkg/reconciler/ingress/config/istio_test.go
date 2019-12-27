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

func TestQualifiedName(t *testing.T) {
	g := Gateway{
		Namespace: "foo",
		Name:      "bar",
	}
	expected := "foo/bar"
	saw := g.QualifiedName()
	if saw != expected {
		t.Errorf("Expected %q, saw %q", expected, saw)
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
			IngressGateways: defaultGateways(),
			LocalGateways:   defaultLocalGateways(),
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
				Namespace:  "knative-testing",
				Name:       "knative-ingress-freeway",
				ServiceURL: "istio-ingressfreeway.istio-system.svc.cluster.local",
			}},
			LocalGateways: defaultLocalGateways(),
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
		name:    "gateway configuration in custom namespace with valid url",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "custom-namespace",
				Name:       "custom-gateway",
				ServiceURL: "istio-ingressfreeway.istio-system.svc.cluster.local",
			}},
			LocalGateways: defaultLocalGateways(),
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-namespace.custom-gateway": "istio-ingressfreeway.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:      "gateway configuration in custom namespace with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-namespace.invalid": "_invalid",
			},
		},
	}, {
		name:    "local gateway configuration with valid url",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: defaultGateways(),
			LocalGateways: []Gateway{{
				Namespace:  "knative-testing",
				Name:       "knative-ingress-backroad",
				ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
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
		name:      "local gateway configuration with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.invalid": "_invalid",
			},
		},
	}, {
		name:    "local gateway configuration in custom namespace with valid url",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: defaultGateways(),
			LocalGateways: []Gateway{{
				Namespace:  "custom-namespace",
				Name:       "custom-local-gateway",
				ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
			}},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.custom-namespace.custom-local-gateway": "istio-ingressbackroad.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:      "local gateway configuration in custom namespace with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.custom-namespace.invalid": "_invalid",
			},
		},
	}, {
		name:    "local gateway configuration with mesh",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: defaultGateways(),
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
