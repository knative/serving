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

	// ActivatorCapacity is the number of the concurrent requests an activator
	// task can accept. This is used in activator subsetting algorithm, to determine
	// the number of activators per revision.
	ActivatorCapacity float64

	// AllowZeroInitialScale indicates whether InitialScale and
	// autoscaling.internal.knative.dev/initialScale are allowed to be set to 0.
	AllowZeroInitialScale bool

	// InitialScale is the cluster-wide default initial revision size for newly deployed
	// services. This can be set to 0 iff AllowZeroInitialScale is true.
	InitialScale int32

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

func defaultConfig() *Config {
	return &Config{
		EnableScaleToZero:                  true,
		EnableGracefulScaledown:            false,
		ContainerConcurrencyTargetFraction: defaultTargetUtilization,
		ContainerConcurrencyTargetDefault:  100,
		// TODO(#1956): Tune target usage based on empirical data.
		TargetUtilization:        defaultTargetUtilization,
		RPSTargetDefault:         200,
		MaxScaleUpRate:           1000,
		MaxScaleDownRate:         2,
		TargetBurstCapacity:      200,
		PanicWindowPercentage:    10,
		ActivatorCapacity:        100,
		PanicThresholdPercentage: 200,
		StableWindow:             60 * time.Second,
		ScaleToZeroGracePeriod:   30 * time.Second,
		TickInterval:             2 * time.Second,
		PodAutoscalerClass:       autoscaling.KPA,
		AllowZeroInitialScale:    false,
		InitialScale:             1,
	}
}

// NewConfigFromMap creates a Config from the supplied map
func NewConfigFromMap(data map[string]string) (*Config, error) {
	lc := defaultConfig()

	// Process bool fields.
	for _, b := range []struct {
		key   string
		field *bool
	}{
		{
			key:   "enable-scale-to-zero",
			field: &lc.EnableScaleToZero,
		},
		{
			key:   "enable-graceful-scaledown",
			field: &lc.EnableGracefulScaledown,
		}, {
			key:   "allow-zero-initial-scale",
			field: &lc.AllowZeroInitialScale,
		}} {
		if raw, ok := data[b.key]; ok {
			*b.field = strings.EqualFold(raw, "true")
		}
	}

	// Process Float64 fields
	for _, f64 := range []struct {
		key   string
		field *float64
	}{{
		key:   "max-scale-up-rate",
		field: &lc.MaxScaleUpRate,
	}, {
		key:   "max-scale-down-rate",
		field: &lc.MaxScaleDownRate,
	}, {
		key:   "container-concurrency-target-percentage",
		field: &lc.ContainerConcurrencyTargetFraction,
	}, {
		key:   "container-concurrency-target-default",
		field: &lc.ContainerConcurrencyTargetDefault,
	}, {
		key:   "requests-per-second-target-default",
		field: &lc.RPSTargetDefault,
	}, {
		key:   "target-burst-capacity",
		field: &lc.TargetBurstCapacity,
	}, {
		key:   "panic-window-percentage",
		field: &lc.PanicWindowPercentage,
	}, {
		key:   "activator-capacity",
		field: &lc.ActivatorCapacity,
	}, {
		key:   "panic-threshold-percentage",
		field: &lc.PanicThresholdPercentage,
	}} {
		if raw, ok := data[f64.key]; ok {
			val, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, err
			}
			*f64.field = val
		}
	}

	// Process int fields
	for _, i := range []struct {
		key   string
		field *int32
	}{{
		key:   "initial-scale",
		field: &lc.InitialScale,
	}} {
		if raw, ok := data[i.key]; ok {
			val, err := strconv.ParseInt(raw, 10, 32)
			if err != nil {
				return nil, err
			}
			*i.field = int32(val)
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
		key   string
		field *time.Duration
	}{{
		key:   "stable-window",
		field: &lc.StableWindow,
	}, {
		key:   "scale-to-zero-grace-period",
		field: &lc.ScaleToZeroGracePeriod,
	}, {
		key:   "tick-interval",
		field: &lc.TickInterval,
	}} {
		if raw, ok := data[dur.key]; ok {
			val, err := time.ParseDuration(raw)
			if err != nil {
				return nil, err
			}
			*dur.field = val
		}
	}

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

	if lc.ActivatorCapacity < 1 {
		return nil, fmt.Errorf("activator-capacity = %v, must be at least 1", lc.ActivatorCapacity)
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

	if lc.InitialScale < 0 || (lc.InitialScale == 0 && !lc.AllowZeroInitialScale) {
		return nil, fmt.Errorf("initial-scale = %v, must be at least 0 (or at least 1 when allow-zero-initial-scale is false)", lc.InitialScale)
	}
	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}
