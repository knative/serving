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

package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"knative.dev/serving/pkg/apis/autoscaling"

	corev1 "k8s.io/api/core/v1"
)

const (
	// ConfigName is the name of the config map of the autoscaler.
	ConfigName = "config-autoscaler"

	// BucketSize is the size of the buckets of stats we create.
	// NB: if this is more than 1s, we need to average values in the
	// metrics buckets.
	BucketSize = 1 * time.Second

	defaultTargetUtilization = 0.7
)

// Config defines the tunable autoscaler parameters
// +k8s:deepcopy-gen=true
type Config struct {
	// Feature flags.
	EnableScaleToZero bool

	// Enable connection-aware pod scaledown
	EnableGracefulScaledown bool

	// Target concurrency knobs for different container concurrency configurations.
	ContainerConcurrencyTargetFraction float64
	ContainerConcurrencyTargetDefault  float64
	// TargetUtilization is used for the metrics other than concurrency. This is not
	// configurable now. Customers can override it by specifying
	// autoscaling.knative.dev/targetUtilizationPercentage in Revision annotation.
	// TODO(yanweiguo): Expose this to config-autoscaler configmap and eventually
	// deprecate ContainerConcurrencyTargetFraction.
	TargetUtilization float64
	// RPSTargetDefault is the default target value for requests per second.
	RPSTargetDefault float64
	// NB: most of our computations are in floats, so this is float to avoid casting.
	TargetBurstCapacity float64

	// General autoscaler algorithm configuration.
	MaxScaleUpRate           float64
	MaxScaleDownRate         float64
	StableWindow             time.Duration
	PanicWindowPercentage    float64
	PanicThresholdPercentage float64
	TickInterval             time.Duration

	ScaleToZeroGracePeriod time.Duration

	PodAutoscalerClass string
}

// NewConfigFromMap creates a Config from the supplied map
func NewConfigFromMap(data map[string]string) (*Config, error) {
	lc := &Config{
		TargetUtilization: defaultTargetUtilization,
	}

	// Process bool fields.
	for _, b := range []struct {
		key          string
		field        *bool
		defaultValue bool
	}{
		{
			key:          "enable-scale-to-zero",
			field:        &lc.EnableScaleToZero,
			defaultValue: true,
		},
		{
			key:          "enable-graceful-scaledown",
			field:        &lc.EnableGracefulScaledown,
			defaultValue: false,
		}} {
		if raw, ok := data[b.key]; !ok {
			*b.field = b.defaultValue
		} else {
			*b.field = strings.EqualFold(raw, "true")
		}
	}

	// Process Float64 fields
	for _, f64 := range []struct {
		key   string
		field *float64
		// specified exactly when optional
		defaultValue float64
	}{{
		key:          "max-scale-up-rate",
		field:        &lc.MaxScaleUpRate,
		defaultValue: 1000.0,
	}, {
		key:          "max-scale-down-rate",
		field:        &lc.MaxScaleDownRate,
		defaultValue: 2.0,
	}, {
		key:   "container-concurrency-target-percentage",
		field: &lc.ContainerConcurrencyTargetFraction,
		// TODO(#1956): Tune target usage based on empirical data.
		defaultValue: defaultTargetUtilization,
	}, {
		key:          "container-concurrency-target-default",
		field:        &lc.ContainerConcurrencyTargetDefault,
		defaultValue: 100.0,
	}, {
		key:          "requests-per-second-target-default",
		field:        &lc.RPSTargetDefault,
		defaultValue: 200.0,
	}, {
		key:          "target-burst-capacity",
		field:        &lc.TargetBurstCapacity,
		defaultValue: 200,
	}, {
		key:          "panic-window-percentage",
		field:        &lc.PanicWindowPercentage,
		defaultValue: 10.0,
	}, {
		key:          "panic-threshold-percentage",
		field:        &lc.PanicThresholdPercentage,
		defaultValue: 200.0,
	}} {
		if raw, ok := data[f64.key]; !ok {
			*f64.field = f64.defaultValue
		} else if val, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, err
		} else {
			*f64.field = val
		}
	}

	// Adjust % ⇒ fractions: for legacy reasons we allow values in the
	// (0, 1] interval, so minimal percentage must be greater than 1.0.
	// Internally we want to have fractions, since otherwise we'll have
	// to perform division on each computation.
	if lc.ContainerConcurrencyTargetFraction > 1.0 {
		lc.ContainerConcurrencyTargetFraction /= 100.0
	}

	// Process Duration fields
	for _, dur := range []struct {
		key          string
		field        *time.Duration
		defaultValue time.Duration
	}{{
		key:          "stable-window",
		field:        &lc.StableWindow,
		defaultValue: 60 * time.Second,
	}, {
		key:          "scale-to-zero-grace-period",
		field:        &lc.ScaleToZeroGracePeriod,
		defaultValue: 30 * time.Second,
	}, {
		key:          "tick-interval",
		field:        &lc.TickInterval,
		defaultValue: 2 * time.Second,
	}} {
		if raw, ok := data[dur.key]; !ok {
			*dur.field = dur.defaultValue
		} else if val, err := time.ParseDuration(raw); err != nil {
			return nil, err
		} else {
			*dur.field = val
		}
	}

	lc.PodAutoscalerClass = autoscaling.KPA
	if pac, ok := data["pod-autoscaler-class"]; ok {
		lc.PodAutoscalerClass = pac
	}

	return validate(lc)
}

func validate(lc *Config) (*Config, error) {
	if lc.ScaleToZeroGracePeriod < autoscaling.WindowMin {
		return nil, fmt.Errorf("scale-to-zero-grace-period must be at least %v, got %v", autoscaling.WindowMin, lc.ScaleToZeroGracePeriod)
	}
	if lc.TargetBurstCapacity < 0 && lc.TargetBurstCapacity != -1 {
		return nil, fmt.Errorf("target-burst-capacity must be non-negative, got %f", lc.TargetBurstCapacity)
	}

	if lc.ContainerConcurrencyTargetFraction <= 0 || lc.ContainerConcurrencyTargetFraction > 1 {
		return nil, fmt.Errorf("container-concurrency-target-percentage = %f is outside of valid range of (0, 100]", lc.ContainerConcurrencyTargetFraction)
	}

	if x := lc.ContainerConcurrencyTargetFraction * lc.ContainerConcurrencyTargetDefault; x < autoscaling.TargetMin {
		return nil, fmt.Errorf("container-concurrency-target-percentage and container-concurrency-target-default yield target concurrency of %v, can't be less than %v", x, autoscaling.TargetMin)
	}

	if lc.RPSTargetDefault < autoscaling.TargetMin {
		return nil, fmt.Errorf("requests-per-second-target-default must be at least %v, got %v", autoscaling.TargetMin, lc.RPSTargetDefault)
	}

	if lc.MaxScaleUpRate <= 1.0 {
		return nil, fmt.Errorf("max-scale-up-rate = %v, must be greater than 1.0", lc.MaxScaleUpRate)
	}

	if lc.MaxScaleDownRate <= 1.0 {
		return nil, fmt.Errorf("max-scale-down-rate = %v, must be greater than 1.0", lc.MaxScaleDownRate)
	}

	// We can't permit stable window be less than our aggregation window for correctness.
	if lc.StableWindow < autoscaling.WindowMin {
		return nil, fmt.Errorf("stable-window = %v, must be at least %v", lc.StableWindow, autoscaling.WindowMin)
	}
	if lc.StableWindow.Round(time.Second) != lc.StableWindow {
		return nil, fmt.Errorf("stable-window = %v, must be specified with at most second precision", lc.StableWindow)
	}

	effPW := time.Duration(lc.PanicWindowPercentage / 100 * float64(lc.StableWindow))
	if effPW < BucketSize || effPW > lc.StableWindow {
		return nil, fmt.Errorf("panic-window-percentage = %v, must be in [%v, 100] interval", lc.PanicWindowPercentage, 100*float64(BucketSize)/float64(lc.StableWindow))
	}

	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}
