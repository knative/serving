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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"cloud.google.com/go/compute/metadata"
	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

const (
	// ConfigName is the name of the configmap
	ConfigName = "config-tracing"

	enableKey               = "enable"
	backendKey              = "backend"
	zipkinEndpointKey       = "zipkin-endpoint"
	debugKey                = "debug"
	sampleRateKey           = "sample-rate"
	stackdriverProjectIDKey = "stackdriver-project-id"
)

// BackendType specifies the backend to use for tracing
type BackendType string

const (
	// None is used for no backend.
	None BackendType = "none"
	// Stackdriver is used for Stackdriver backend.
	Stackdriver BackendType = "stackdriver"
	// Zipkin is used for Zipkin backend.
	Zipkin BackendType = "zipkin"
)

// Config holds the configuration for tracers
type Config struct {
	Backend              BackendType
	ZipkinEndpoint       string
	StackdriverProjectID string

	Debug      bool
	SampleRate float64
}

// Equals returns true if two Configs are identical
func (cfg *Config) Equals(other *Config) bool {
	return reflect.DeepEqual(other, cfg)
}

func defaultConfig() *Config {
	return &Config{
		Backend:    None,
		Debug:      false,
		SampleRate: 0.1,
	}
}

// NewTracingConfigFromMap returns a Config given a map corresponding to a ConfigMap
func NewTracingConfigFromMap(cfgMap map[string]string) (*Config, error) {
	tc := defaultConfig()

	if backend, ok := cfgMap[backendKey]; ok {
		switch bt := BackendType(backend); bt {
		case Stackdriver, Zipkin, None:
			tc.Backend = bt
		default:
			return nil, fmt.Errorf("unsupported tracing backend value %q", backend)
		}
	} else if enable, ok := cfgMap[enableKey]; ok {
		// For backwards compatibility, parse the enabled flag as Zipkin.
		enableBool, err := strconv.ParseBool(enable)
		if err != nil {
			return nil, fmt.Errorf("failed parsing tracing config %q: %w", enableKey, err)
		}
		if enableBool {
			tc.Backend = Zipkin
		}
	}

	if err := cm.Parse(cfgMap,
		cm.AsString(zipkinEndpointKey, &tc.ZipkinEndpoint),
		cm.AsString(stackdriverProjectIDKey, &tc.StackdriverProjectID),
		cm.AsBool(debugKey, &tc.Debug),
		cm.AsFloat64(sampleRateKey, &tc.SampleRate),
	); err != nil {
		return nil, err
	}

	if tc.Backend == Zipkin && tc.ZipkinEndpoint == "" {
		return nil, errors.New("zipkin tracing enabled without a zipkin endpoint specified")
	}

	if tc.Backend == Stackdriver && tc.StackdriverProjectID == "" {
		projectID, err := metadata.ProjectID()
		if err != nil {
			return nil, fmt.Errorf("stackdriver tracing enabled without a project-id specified: %w", err)
		}
		tc.StackdriverProjectID = projectID
	}

	if tc.SampleRate < 0 || tc.SampleRate > 1 {
		return nil, fmt.Errorf("sample-rate = %v must be in [0, 1] range", tc.SampleRate)
	}

	return tc, nil
}

// NewTracingConfigFromConfigMap returns a Config for the given configmap
func NewTracingConfigFromConfigMap(config *corev1.ConfigMap) (*Config, error) {
	return NewTracingConfigFromMap(config.Data)
}

// JsonToTracingConfig converts a json string of a Config.
// Returns a non-nil Config always and an eventual error.
func JsonToTracingConfig(jsonCfg string) (*Config, error) {
	if jsonCfg == "" {
		return defaultConfig(), errors.New("empty json tracing config")
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(jsonCfg), &configMap); err != nil {
		return defaultConfig(), err
	}

	cfg, err := NewTracingConfigFromMap(configMap)
	if err != nil {
		return defaultConfig(), nil
	}
	return cfg, nil
}

// TracingConfigToJson converts a Config to a json string.
func TracingConfigToJson(cfg *Config) (string, error) {
	if cfg == nil {
		return "", nil
	}

	out := make(map[string]string, 5)
	out[backendKey] = string(cfg.Backend)
	if cfg.ZipkinEndpoint != "" {
		out[zipkinEndpointKey] = cfg.ZipkinEndpoint
	}
	if cfg.StackdriverProjectID != "" {
		out[stackdriverProjectIDKey] = cfg.StackdriverProjectID
	}
	out[debugKey] = fmt.Sprint(cfg.Debug)
	out[sampleRateKey] = fmt.Sprint(cfg.SampleRate)

	jsonCfg, err := json.Marshal(out)
	return string(jsonCfg), err
}
