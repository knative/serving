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
	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/reconciler/autoscaling/resources"
)

// Deciders is an interface for notifying the presence or absence of autoscaling deciders.
type Deciders interface {
	// Get accesses the Decider resource for this key, returning any errors.
	Get(ctx context.Context, namespace, name string) (*autoscaler.Decider, error)

	// Create adds a Decider resource for a given key, returning any errors.
	Create(ctx context.Context, decider *autoscaler.Decider) (*autoscaler.Decider, error)

	// Delete removes the Decider resource for a given key, returning any errors.
	Delete(ctx context.Context, namespace, name string) error

	// Watch registers a function to call when Decider change.
	Watch(watcher func(types.NamespacedName))

	// Update update the Decider resource, return the new Decider or any errors.
	Update(ctx context.Context, decider *autoscaler.Decider) (*autoscaler.Decider, error)
}

// MakeDecider constructs a Decider resource from a PodAutoscaler taking
// into account the PA's ContainerConcurrency and the relevant
// autoscaling annotation.
func MakeDecider(ctx context.Context, pa *v1alpha1.PodAutoscaler, config *autoscaler.Config, svc string) *autoscaler.Decider {
	panicThresholdPercentage := config.PanicThresholdPercentage
	if x, ok := pa.PanicThresholdPercentage(); ok {
		panicThresholdPercentage = x
	}

	target, total := resources.ResolveMetricTarget(pa, config)
	panicThreshold := target * panicThresholdPercentage / 100.0

	tbc := config.TargetBurstCapacity
	if x, ok := pa.TargetBC(); ok {
		tbc = x
	}
	return &autoscaler.Decider{
		ObjectMeta: *pa.ObjectMeta.DeepCopy(),
		Spec: autoscaler.DeciderSpec{
			TickInterval:        config.TickInterval,
			MaxScaleUpRate:      config.MaxScaleUpRate,
			MaxScaleDownRate:    config.MaxScaleDownRate,
			ScalingMetric:       pa.Metric(),
			TargetValue:         target,
			TotalValue:          total,
			TargetBurstCapacity: tbc,
			PanicThreshold:      panicThreshold,
			StableWindow:        resources.StableWindow(pa, config),
			ServiceName:         svc,
		},
	}
}
