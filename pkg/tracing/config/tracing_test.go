/*
Copyright 2019 The Knative Authors.

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
)

func TestEquals(t *testing.T) {
	tt := []struct {
		name   string
		cmp1   Config
		cmp2   Config
		expect bool
	}{{
		name:   "Default",
		cmp1:   Config{},
		cmp2:   Config{},
		expect: true,
	}, {
		name:   "Unequal",
		cmp1:   Config{Enable: true},
		cmp2:   Config{},
		expect: false,
	}}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if res := tc.cmp1.Equals(&tc.cmp2); res != tc.expect {
				t.Errorf("Expected %v, got %v", tc.expect, res)
			}
		})
	}
}

func TestNewConfigFromMap(t *testing.T) {
	tt := []struct {
		name   string
		input  map[string]string
		output Config
	}{{
		name:  "Empty map",
		input: map[string]string{},
		output: Config{
			SampleRate: 0.1,
		},
	}, {
		name: "Everything enabled",
		input: map[string]string{
			enableKey:         "true",
			zipkinEndpointKey: "some-endpoint",
			debugKey:          "true",
			sampleRateKey:     "0.5",
		},
		output: Config{
			Enable:         true,
			Debug:          true,
			ZipkinEndpoint: "some-endpoint",
			SampleRate:     0.5,
		},
	}}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if cfg, err := NewTracingConfigFromMap(tc.input); err != nil {
				t.Errorf("Failed to create tracing config: %v", err)
			} else {
				if diff := cmp.Diff(&tc.output, cfg); diff != "" {
					t.Errorf("Got config from map (-want, +got) = %v", diff)
				}
			}
		})
	}
}

func TestConfigFromConfigMap(t *testing.T) {
	cfg, err := NewTracingConfigFromConfigMap(&corev1.ConfigMap{
		Data: map[string]string{
			zipkinEndpointKey: "some-endpoint",
		},
	})
	if err != nil {
		t.Errorf("failed to create config from config map: %v", err)
	}

	if cfg.ZipkinEndpoint != "some-endpoint" {
		t.Errorf("returned config does not have matching endpoint url: %v", cfg)
	}
}
