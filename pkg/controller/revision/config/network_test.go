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
	"github.com/knative/serving/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewNetworkNoEntry(t *testing.T) {
	c, err := NewNetworkFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      NetworkConfigName,
		},
	})
	if err != nil {
		t.Errorf("NewNetworkFromConfigMap() = %v", err)
	}
	if len(c.IstioOutboundIPRanges) > 0 {
		t.Error("Expected an empty value when config map doesn't have the entry.")
	}
}

func TestNewNetwork(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{{
		in:   "10.10.10.0/24", // Valid single outbound IP range
		want: "10.10.10.0/24",
	}, {
		in:   "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16", // Valid multiple outbound IP ranges
		want: "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
	}, {
		in:   "*",
		want: "*",
	}, {
		in:   "",
		want: "",
	}, {
		in:   " *   ",
		want: "*",
	}, {
		in:   "  ",
		want: "",
	}, {
		in:   " ,   ,  , ",
		want: "",
	}, {
		in:   "  10.10.2.3/24\t",
		want: "10.10.2.3/24",
	}, {
		in:   "10.10.10.0/24,  10.240.10.0/14 \r,  192.192.10.0/16 ",
		want: "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
	}, {
		in:   " \t\t10.10.10.0/24,  ,,\t\n\r\n,10.240.10.0/14\n,   192.192.10.0/16",
		want: "10.10.10.0/24,10.240.10.0/14,192.192.10.0/16",
	}}

	for _, tt := range tests {
		c, err := NewNetworkFromConfigMap(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      NetworkConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: tt.in,
			},
		})
		if err != nil {
			t.Errorf("NewNetworkFromConfigMap() = %v", err)
		}
		if c.IstioOutboundIPRanges != tt.want {
			t.Errorf("Want %v, got %v", tt.want, c.IstioOutboundIPRanges)
		}
	}
}

func TestBadNetwork(t *testing.T) {
	invalidList := []string{
		"10.10.10.10/33",         // Invalid outbound IP range
		"10.10.10.10/12,invalid", // Some valid, some invalid ranges
		"10.10.10.10/12,-1.1.1.1/10",
		"*,",
		"*,*",
		"*,   *",
		"this is not an IP range",
	}
	for _, invalid := range invalidList {
		c, err := NewNetworkFromConfigMap(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace,
				Name:      NetworkConfigName,
			},
			Data: map[string]string{
				IstioOutboundIPRangesKey: invalid,
			},
		})
		if err == nil {
			t.Errorf("NewNetworkFromConfigMap() = %v, wanted error", c)
		}
	}
}

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
