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
	"time"

	cm "knative.dev/pkg/configmap"
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

	// MaxScale is the default max scale for any revision created without an
	// autoscaling.knative.dev/maxScale annotation
	MaxScale int32

	// General autoscaler algorithm configuration.
	MaxScaleUpRate           float64
	MaxScaleDownRate         float64
	StableWindow             time.Duration
	PanicWindowPercentage    float64
	PanicThresholdPercentage float64

	ScaleToZeroGracePeriod        time.Duration
	ScaleToZeroPodRetentionPeriod time.Duration

	PodAutoscalerClass string
}

func defaultConfig() *Config {
	return &Config{
		EnableScaleToZero:                  true,
		ContainerConcurrencyTargetFraction: defaultTargetUtilization,
		ContainerConcurrencyTargetDefault:  100,
		// TODO(#1956): Tune target usage based on empirical data.
		TargetUtilization:             defaultTargetUtilization,
		RPSTargetDefault:              200,
		MaxScaleUpRate:                1000,
		MaxScaleDownRate:              2,
		TargetBurstCapacity:           200,
		PanicWindowPercentage:         10,
		ActivatorCapacity:             100,
		PanicThresholdPercentage:      200,
		StableWindow:                  60 * time.Second,
		ScaleToZeroGracePeriod:        30 * time.Second,
		ScaleToZeroPodRetentionPeriod: 0 * time.Second,
		PodAutoscalerClass:            autoscaling.KPA,
		AllowZeroInitialScale:         false,
		InitialScale:                  1,
		MaxScale:                      0,
	}
}

// NewConfigFromMap creates a Config from the supplied map
func NewConfigFromMap(data map[string]string) (*Config, error) {
	lc := defaultConfig()

	if err := cm.Parse(data,
		cm.AsString("pod-autoscaler-class", &lc.PodAutoscalerClass),

		cm.AsBool("enable-scale-to-zero", &lc.EnableScaleToZero),
		cm.AsBool("allow-zero-initial-scale", &lc.AllowZeroInitialScale),

		cm.AsFloat64("max-scale-up-rate", &lc.MaxScaleUpRate),
		cm.AsFloat64("max-scale-down-rate", &lc.MaxScaleDownRate),
		cm.AsFloat64("container-concurrency-target-percentage", &lc.ContainerConcurrencyTargetFraction),
		cm.AsFloat64("container-concurrency-target-default", &lc.ContainerConcurrencyTargetDefault),
		cm.AsFloat64("requests-per-second-target-default", &lc.RPSTargetDefault),
		cm.AsFloat64("target-burst-capacity", &lc.TargetBurstCapacity),
		cm.AsFloat64("panic-window-percentage", &lc.PanicWindowPercentage),
		cm.AsFloat64("activator-capacity", &lc.ActivatorCapacity),
		cm.AsFloat64("panic-threshold-percentage", &lc.PanicThresholdPercentage),

		cm.AsInt32("initial-scale", &lc.InitialScale),
		cm.AsInt32("max-scale", &lc.MaxScale),

		cm.AsDuration("stable-window", &lc.StableWindow),
		cm.AsDuration("scale-to-zero-grace-period", &lc.ScaleToZeroGracePeriod),
		cm.AsDuration("scale-to-zero-pod-retention-period", &lc.ScaleToZeroPodRetentionPeriod),
	); err != nil {
		return nil, fmt.Errorf("failed to parse data: %w", err)
	}

	// Adjust % ⇒ fractions: for legacy reasons we allow values in the
	// (0, 1] interval, so minimal percentage must be greater than 1.0.
	// Internally we want to have fractions, since otherwise we'll have
	// to perform division on each computation.
	if lc.ContainerConcurrencyTargetFraction > 1.0 {
		lc.ContainerConcurrencyTargetFraction /= 100.0
	}

	return validate(lc)
}

func validate(lc *Config) (*Config, error) {
	if lc.ScaleToZeroGracePeriod < autoscaling.WindowMin {
		return nil, fmt.Errorf("scale-to-zero-grace-period must be at least %v, got %v", autoscaling.WindowMin, lc.ScaleToZeroGracePeriod)
	}

	if lc.ScaleToZeroPodRetentionPeriod < 0 {
		return nil, fmt.Errorf("scale-to-zero-pod-retention-period cannot be negative, was: %v", lc.ScaleToZeroPodRetentionPeriod)
	}

	if lc.TargetBurstCapacity < 0 && lc.TargetBurstCapacity != -1 {
		return nil, fmt.Errorf("target-burst-capacity must be either non-negative or -1 (for unlimited), got %f", lc.TargetBurstCapacity)
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
	// Or too big, so that our desisions are too imprecise.
	if lc.StableWindow < autoscaling.WindowMin || lc.StableWindow > autoscaling.WindowMax {
		return nil, fmt.Errorf("stable-window = %v, must be in [%v; %v] range", lc.StableWindow,
			autoscaling.WindowMin, autoscaling.WindowMax)
	}

	if lc.StableWindow.Round(time.Second) != lc.StableWindow {
		return nil, fmt.Errorf("stable-window = %v, must be specified with at most second precision", lc.StableWindow)
	}

	// We ensure BucketSize in the `MakeMetric`, so just ensure percentage is in the correct region.
	if lc.PanicWindowPercentage < autoscaling.PanicWindowPercentageMin ||
		lc.PanicWindowPercentage > autoscaling.PanicWindowPercentageMax {
		return nil, fmt.Errorf("panic-window-percentage = %v, must be in [%v, %v] interval",
			lc.PanicWindowPercentage, autoscaling.PanicWindowPercentageMin, autoscaling.PanicWindowPercentageMax)

	}

	if lc.InitialScale < 0 || (lc.InitialScale == 0 && !lc.AllowZeroInitialScale) {
		return nil, fmt.Errorf("initial-scale = %v, must be at least 0 (or at least 1 when allow-zero-initial-scale is false)", lc.InitialScale)
	}

	if lc.MaxScale < 0 {
		return nil, fmt.Errorf("max-scale = %v, must be at least 0", lc.MaxScale)
	}
	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}
