/*
Copyright 2018 The Knative Authors
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

package autoscaler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	ConfigName = "config-autoscaler"
)

// Config defines the tunable autoscaler parameters
type Config struct {
	// Feature flags.
	EnableScaleToZero bool
	EnableVPA         bool

	// Target concurrency knobs for different concurrency modes.
	SingleTargetConcurrency   float64
	MultiTargetConcurrency    float64
	VPAMultiTargetConcurrency float64

	// General autoscaler algorithm configuration.
	MaxScaleUpRate           float64
	StableWindow             time.Duration
	PanicWindow              time.Duration
	ScaleToZeroThreshold     time.Duration
	ConcurrencyQuantumOfTime time.Duration
}

func (c *Config) TargetConcurrency(model v1alpha1.RevisionRequestConcurrencyModelType) float64 {
	switch model {
	case v1alpha1.RevisionRequestConcurrencyModelSingle:
		return c.SingleTargetConcurrency
	case v1alpha1.RevisionRequestConcurrencyModelMulti:
		if c.EnableVPA {
			return c.VPAMultiTargetConcurrency
		} else {
			return c.MultiTargetConcurrency
		}
	default:
		return c.MultiTargetConcurrency
	}
}

// NewConfigFromMap creates a Config from the supplied map
func NewConfigFromMap(data map[string]string) (*Config, error) {
	lc := &Config{}

	// Process bool fields
	for _, b := range []struct {
		key   string
		field *bool
	}{{
		key:   "enable-scale-to-zero",
		field: &lc.EnableScaleToZero,
	}, {
		key:   "enable-vertical-pod-autoscaling",
		field: &lc.EnableVPA,
	}} {
		if raw, ok := data[b.key]; !ok {
			*b.field = false
		} else {
			*b.field = (strings.ToLower(raw) == "true")
		}
	}

	// Process Float64 fields
	for _, f64 := range []struct {
		key      string
		field    *float64
		optional bool
		// specified exactyl when optional
		defaultValue float64
	}{{
		key:   "max-scale-up-rate",
		field: &lc.MaxScaleUpRate,
	}, {
		key:   "single-concurrency-target",
		field: &lc.SingleTargetConcurrency,
	}, {
		key:   "multi-concurrency-target",
		field: &lc.MultiTargetConcurrency,
	}, {
		key:          "vpa-multi-concurrency-target",
		field:        &lc.VPAMultiTargetConcurrency,
		optional:     true,
		defaultValue: 10.0,
	}} {
		if raw, ok := data[f64.key]; !ok {
			if f64.optional {
				*f64.field = f64.defaultValue
				continue
			}
			return nil, fmt.Errorf("Autoscaling configmap is missing %q", f64.key)
		} else if val, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, err
		} else {
			*f64.field = val
		}
	}

	// Process Duration fields
	for _, dur := range []struct {
		key   string
		field *time.Duration
	}{{
		key:   "stable-window",
		field: &lc.StableWindow,
	}, {
		key:   "panic-window",
		field: &lc.PanicWindow,
	}, {
		key:   "scale-to-zero-threshold",
		field: &lc.ScaleToZeroThreshold,
	}, {
		key:   "concurrency-quantum-of-time",
		field: &lc.ConcurrencyQuantumOfTime,
	}} {
		if raw, ok := data[dur.key]; !ok {
			return nil, fmt.Errorf("Autoscaling configmap is missing %q", dur.key)
		} else if val, err := time.ParseDuration(raw); err != nil {
			return nil, err
		} else {
			*dur.field = val
		}
	}

	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}
