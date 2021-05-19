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
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"

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

func defaultConfig() *autoscalerconfig.Config {
	return &autoscalerconfig.Config{
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
		ScaleDownDelay:                0 * time.Second,
		PodAutoscalerClass:            autoscaling.KPA,
		AllowZeroInitialScale:         false,
		InitialScale:                  1,
		MaxScale:                      0,
		MaxScaleLimit:                 0,
	}
}

// NewConfigFromMap creates a Config from the supplied map
func NewConfigFromMap(data map[string]string) (*autoscalerconfig.Config, error) {
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
		cm.AsInt32("max-scale-limit", &lc.MaxScaleLimit),

		cm.AsDuration("stable-window", &lc.StableWindow),
		cm.AsDuration("scale-down-delay", &lc.ScaleDownDelay),
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

func validate(lc *autoscalerconfig.Config) (*autoscalerconfig.Config, error) {
	if lc.ScaleToZeroGracePeriod <= 0 {
		return nil, fmt.Errorf("scale-to-zero-grace-period must be positive, was: %v", lc.ScaleToZeroGracePeriod)
	}

	if lc.ScaleDownDelay < 0 {
		return nil, fmt.Errorf("scale-down-delay cannot be negative, was: %v", lc.ScaleDownDelay)
	}

	if lc.ScaleDownDelay.Round(time.Second) != lc.ScaleDownDelay {
		return nil, fmt.Errorf("scale-down-delay = %v, must be specified with at most second precision", lc.ScaleDownDelay)
	}

	if lc.ScaleToZeroPodRetentionPeriod < 0 {
		return nil, fmt.Errorf("scale-to-zero-pod-retention-period cannot be negative, was: %v", lc.ScaleToZeroPodRetentionPeriod)
	}

	if lc.TargetBurstCapacity < 0 && lc.TargetBurstCapacity != -1 {
		return nil, fmt.Errorf("target-burst-capacity must be either non-negative or -1 (for unlimited), was: %f", lc.TargetBurstCapacity)
	}

	if lc.ContainerConcurrencyTargetFraction <= 0 || lc.ContainerConcurrencyTargetFraction > 1 {
		return nil, fmt.Errorf("container-concurrency-target-percentage = %f is outside of valid range of (0, 100]", lc.ContainerConcurrencyTargetFraction)
	}

	if x := lc.ContainerConcurrencyTargetFraction * lc.ContainerConcurrencyTargetDefault; x < autoscaling.TargetMin {
		return nil, fmt.Errorf("container-concurrency-target-percentage and container-concurrency-target-default yield target concurrency of %v, can't be less than %v", x, autoscaling.TargetMin)
	}

	if lc.RPSTargetDefault < autoscaling.TargetMin {
		return nil, fmt.Errorf("requests-per-second-target-default must be at least %v, was: %v", autoscaling.TargetMin, lc.RPSTargetDefault)
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
	// Or too big, so that our decisions are too imprecise.
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

	var minMaxScale int32
	if lc.MaxScaleLimit > 0 {
		// Default maxScale must be set if maxScaleLimit is set.
		minMaxScale = 1
	}

	if lc.MaxScale < minMaxScale || (lc.MaxScaleLimit > 0 && lc.MaxScale > lc.MaxScaleLimit) {
		return nil, fmt.Errorf("max-scale = %d, must be in [%d, max-scale-limit(%d)] range",
			lc.MaxScale, minMaxScale, lc.MaxScaleLimit)
	}

	if lc.MaxScaleLimit < 0 {
		return nil, fmt.Errorf("max-scale-limit = %v, must be at least 0", lc.MaxScaleLimit)
	}

	return lc, nil
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*autoscalerconfig.Config, error) {
	return NewConfigFromMap(configMap.Data)
}
