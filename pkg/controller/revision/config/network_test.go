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
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOurNetwork(t *testing.T) {
	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", NetworkConfigName))
	if err != nil {
		t.Errorf("ReadFile() = %v", err)
	}
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Errorf("yaml.Unmarshal() = %v", err)
	}
	if _, err := NewNetworkFromConfigMap(&cm); err != nil {
		t.Errorf("NewNetworkFromConfigMap() = %v", err)
	}
}

var networkConfigTests = []struct {
	name           string
	wantErr        bool
	wantController interface{}
	config         *corev1.ConfigMap
}{{
	"network configuration with no network input",
	false,
	&Network{},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
	}}, {
	"network configuration with invalid outbound IP range",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "10.10.10.10/33",
		},
	}}, {
	"network configuration with empty network",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "",
		},
	}}, {
	"network configuration with both valid and some invalid range",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "10.10.10.10/12,invalid",
		},
	}}, {
	"network configuration with invalid network range",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "10.10.10.10/12,-1.1.1.1/10",
		},
	}}, {
	"network configuration with invalid network key",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "this is not an IP range",
		},
	}}, {
	"network configuration with invalid network",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "*,*",
		},
	}}, {
	"network configuration with incomplete network array",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "*,",
		},
	}}, {
	"network configuration with invalid network string",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: ", ,",
		},
	}}, {
	"network configuration with invalid network string",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: ",,",
		},
	}}, {
	"network configuration with invalid network range",
	true,
	(*Network)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: ",",
		},
	}}, {
	"network configuration with valid CIDR network range",
	false,
	&Network{
		IstioOutboundIPRanges: "10.10.10.0/24",
	},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "10.10.10.0/24",
		},
	}}, {
	"network configuration with multiple valid network ranges",
	false,
	&Network{
		IstioOutboundIPRanges: "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
	},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
		},
	}}, {
	"network configuration with valid network",
	false,
	&Network{
		IstioOutboundIPRanges: "*",
	},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: "*",
		},
	}},
}

func TestNetworkConfig(t *testing.T) {
	for _, tt := range networkConfigTests {
		actualController, err := NewNetworkFromConfigMap(tt.config)

		if (err != nil) != tt.wantErr {
			t.Fatalf("Test: %q; NewNetworkFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
		}

		if diff := cmp.Diff(actualController, tt.wantController); diff != "" {
			t.Fatalf("Test: %q; want %v, but got %v", tt.name, tt.wantController, actualController)
		}
	}
}
