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
	// ConfigName is the name of the config map of the autoscaler.
	ConfigName = "config-autoscaler"
)

// Config defines the tunable autoscaler parameters
// +k8s:deepcopy-gen=true
type Config struct {
	// Feature flags.
	EnableScaleToZero bool
	EnableVPA         bool

	// Target concurrency knobs for different container concurrency configurations.
	ContainerConcurrencyTargetPercentage float64
	ContainerConcurrencyTargetDefault    float64

	// General autoscaler algorithm configuration.
	MaxScaleUpRate float64
	StableWindow   time.Duration
	PanicWindow    time.Duration
	TickInterval   time.Duration

	ScaleToZeroThreshold   time.Duration
	ScaleToZeroGracePeriod time.Duration
	// This is computed by ScaleToZeroThreshold - ScaleToZeroGracePeriod
	ScaleToZeroIdlePeriod time.Duration
}

// TargetConcurrency calculates the target concurrency for a given container-concurrency
// taking the container-concurrency-target-percentage into account.
func (c *Config) TargetConcurrency(concurrency v1alpha1.RevisionContainerConcurrencyType) float64 {
	if concurrency == 0 {
		return c.ContainerConcurrencyTargetDefault
	}
	return float64(concurrency) * c.ContainerConcurrencyTargetPercentage
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
			*b.field = strings.ToLower(raw) == "true"
		}
	}

	// Process Float64 fields
	for _, f64 := range []struct {
		key      string
		field    *float64
		optional bool
		// specified exactly when optional
		defaultValue float64
	}{{
		key:   "max-scale-up-rate",
		field: &lc.MaxScaleUpRate,
	}, {
		key:   "container-concurrency-target-percentage",
		field: &lc.ContainerConcurrencyTargetPercentage,
	}, {
		key:   "container-concurrency-target-default",
		field: &lc.ContainerConcurrencyTargetDefault,
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
		key      string
		field    *time.Duration
		optional bool
		// specified exactly when optional
		defaultValue time.Duration
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
		key:          "scale-to-zero-grace-period",
		field:        &lc.ScaleToZeroGracePeriod,
		optional:     true,
		defaultValue: 2 * time.Minute,
	}, {
		key:   "tick-interval",
		field: &lc.TickInterval,
	}} {
		if raw, ok := data[dur.key]; !ok {
			if dur.optional {
				*dur.field = dur.defaultValue
				continue
			}
			return nil, fmt.Errorf("Autoscaling configmap is missing %q", dur.key)
		} else if val, err := time.ParseDuration(raw); err != nil {
			return nil, err
		} else {
			*dur.field = val
		}
	}

	if lc.ScaleToZeroGracePeriod < 30*time.Second {
		return nil, fmt.Errorf("scale-to-zero-grace-period must be at least 30s, got %v", lc.ScaleToZeroGracePeriod)
	}

	lc.ScaleToZeroIdlePeriod = lc.ScaleToZeroThreshold - lc.ScaleToZeroGracePeriod
	if lc.ScaleToZeroIdlePeriod < 30*time.Second {
		return nil, fmt.Errorf("scale-to-zero-threshold minus scale-to-zero-grace-period must be at least 30s, got %v (%v - %v)",
			lc.ScaleToZeroIdlePeriod, lc.ScaleToZeroThreshold, lc.ScaleToZeroGracePeriod)
	}

	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}
