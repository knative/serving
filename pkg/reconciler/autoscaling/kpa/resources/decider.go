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

package resources

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	asv1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/autoscaler/scaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/resources"
)

// Deciders is an interface for notifying the presence or absence of autoscaling deciders.
type Deciders interface {
	// Get accesses the Decider resource for this key, returning any errors.
	Get(ctx context.Context, namespace, name string) (*scaling.Decider, error)

	// Create adds a Decider resource for a given key, returning any errors.
	Create(ctx context.Context, decider *scaling.Decider) (*scaling.Decider, error)

	// Delete removes the Decider resource for a given key, returning any errors.
	Delete(ctx context.Context, namespace, name string)

	// Watch registers a function to call when Decider change.
	Watch(watcher func(types.NamespacedName))

	// Update update the Decider resource, return the new Decider or any errors.
	Update(ctx context.Context, decider *scaling.Decider) (*scaling.Decider, error)
}

// MakeDecider constructs a Decider resource from a PodAutoscaler taking
// into account the PA's ContainerConcurrency and the relevant
// autoscaling annotation.
func MakeDecider(_ context.Context, pa *asv1a1.PodAutoscaler, config *autoscalerconfig.Config) *scaling.Decider {
	panicThresholdPercentage := config.PanicThresholdPercentage
	if x, ok := pa.PanicThresholdPercentage(); ok {
		panicThresholdPercentage = x
	}

	target, total := resources.ResolveMetricTarget(pa, config)
	panicThreshold := panicThresholdPercentage / 100.0

	tbc := config.TargetBurstCapacity
	if x, ok := pa.TargetBC(); ok {
		tbc = x
	}

	scaleDownDelay := config.ScaleDownDelay
	if sdd, ok := pa.ScaleDownDelay(); ok {
		scaleDownDelay = sdd
	}

	return &scaling.Decider{
		ObjectMeta: *pa.ObjectMeta.DeepCopy(),
		Spec: scaling.DeciderSpec{
			MaxScaleUpRate:      config.MaxScaleUpRate,
			MaxScaleDownRate:    config.MaxScaleDownRate,
			ScalingMetric:       pa.Metric(),
			TargetValue:         target,
			TotalValue:          total,
			TargetBurstCapacity: tbc,
			ActivatorCapacity:   config.ActivatorCapacity,
			PanicThreshold:      panicThreshold,
			StableWindow:        resources.StableWindow(pa, config),
			ScaleDownDelay:      scaleDownDelay,
			InitialScale:        GetInitialScale(config, pa),
			Reachable:           pa.Spec.Reachability != asv1a1.ReachabilityUnreachable,
		},
	}
}

// GetInitialScale returns the calculated initial scale based on the autoscaler
// ConfigMap and PA initial scale annotation value.
func GetInitialScale(asConfig *autoscalerconfig.Config, pa *asv1a1.PodAutoscaler) int32 {
	initialScale := asConfig.InitialScale
	revisionInitialScale, ok := pa.InitialScale()
	if !ok || (revisionInitialScale == 0 && !asConfig.AllowZeroInitialScale) {
		return initialScale
	}
	return revisionInitialScale
}
