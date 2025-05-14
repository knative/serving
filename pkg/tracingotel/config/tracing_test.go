/*
Copyright 2022 The Knative Authors

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
		cmp1:   Config{Backend: Zipkin},
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

func TestNewConfigSuccess(t *testing.T) {
	tt := []struct {
		name   string
		input  map[string]string
		output *Config
	}{{
		name:   "Empty map",
		input:  map[string]string{},
		output: NoopConfig(),
	}, {
		name: "Everything enabled (legacy)",
		input: map[string]string{
			enableKey:         "true",
			zipkinEndpointKey: "some-endpoint",
			debugKey:          "true",
			sampleRateKey:     "0.5",
		},
		output: &Config{
			Backend:        Zipkin,
			Debug:          true,
			ZipkinEndpoint: "some-endpoint",
			SampleRate:     0.5,
		},
	}, {
		name: "Everything enabled (zipkin)",
		input: map[string]string{
			backendKey:        "zipkin",
			zipkinEndpointKey: "some-endpoint",
			debugKey:          "true",
			sampleRateKey:     "0.5",
		},
		output: &Config{
			Backend:        Zipkin,
			Debug:          true,
			ZipkinEndpoint: "some-endpoint",
			SampleRate:     0.5,
		},
	}}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := NewTracingConfigFromConfigMap(&corev1.ConfigMap{
				Data: tc.input,
			})
			if err != nil {
				t.Fatal("Failed to create tracing config:", err)
			}
			if diff := cmp.Diff(tc.output, cfg); diff != "" {
				t.Error("Got config from map (-want, +got) =", diff)
			}
		})
	}
}

func TestNewConfigJSON(t *testing.T) {
	tt := []struct {
		name   string
		input  map[string]string
		output *Config
	}{{
		name:   "Empty map",
		input:  map[string]string{},
		output: NoopConfig(),
	}, {
		name: "Everything enabled (legacy)",
		input: map[string]string{
			enableKey:         "true",
			zipkinEndpointKey: "some-endpoint",
			debugKey:          "true",
			sampleRateKey:     "0.5",
		},
		output: &Config{
			Backend:        Zipkin,
			Debug:          true,
			ZipkinEndpoint: "some-endpoint",
			SampleRate:     0.5,
		},
	}, {
		name: "Everything enabled (zipkin)",
		input: map[string]string{
			backendKey:        "zipkin",
			zipkinEndpointKey: "some-endpoint",
			debugKey:          "true",
			sampleRateKey:     "0.5",
		},
		output: &Config{
			Backend:        Zipkin,
			Debug:          true,
			ZipkinEndpoint: "some-endpoint",
			SampleRate:     0.5,
		},
	}}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			config, err := NewTracingConfigFromMap(tc.input)
			if err != nil {
				t.Fatal("Failed to create tracing config:", err)
			}

			json, err := TracingConfigToJSON(config)
			if err != nil {
				t.Fatal("Failed to create tracing config:", err)
			}

			haveConfig, err := JSONToTracingConfig(json)
			if err != nil {
				t.Fatal("Failed to create tracing config:", err)
			}

			if !cmp.Equal(tc.output, haveConfig) {
				t.Error("Got config from map (-want, +got) =", cmp.Diff(tc.output, haveConfig))
			}
		})
	}
}

func TestNewConfigFailures(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]string
	}{{
		name: "bad key",
		input: map[string]string{
			backendKey: "jaeger",
		},
	}, {
		name: "bad sampling rate",
		input: map[string]string{
			sampleRateKey: "not a number",
		},
	}, {
		name: "sampling rate too low",
		input: map[string]string{
			sampleRateKey: "-0.1",
		},
	}, {
		name: "sampling rate too high",
		input: map[string]string{
			sampleRateKey: "1.01",
		},
	}, {
		name: "zipkin set without backend",
		input: map[string]string{
			backendKey: "zipkin",
		},
	}, {
		name: "zipkin set without backend legacy",
		input: map[string]string{
			enableKey: "true",
		},
	}, {
		name: "legacy key invalid",
		input: map[string]string{
			enableKey: "dot dash dot",
		},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewTracingConfigFromMap(tc.input); err == nil {
				t.Error("Expected bad input to fail")
			}
		})
	}
}
