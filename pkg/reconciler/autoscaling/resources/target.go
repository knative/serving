/*
Copyright 2019 The Knative Authors

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
	"math"

	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
)

// ResolveMetricTarget takes scaling metric knobs from multiple locations
// and resolves them to the final value to be used by the autoscaler.
// `target` is the target value of scaling metric that we autoscaler will aim for;
// `total` is the maximum possible value of scaling metric that is permitted on the pod.
func ResolveMetricTarget(pa *autoscalingv1alpha1.PodAutoscaler, config *autoscalerconfig.Config) (target, total float64) {
	tu := 0.

	switch pa.Metric() {
	case autoscaling.RPS:
		total = config.RPSTargetDefault
		tu = config.TargetUtilization
	case autoscaling.Memory:
		// Superpod autoscaling based on memory requests
		if annotationTarget, ok := pa.Target(); ok {
			// If the target is set in the annotation, use it as the target.
			// Total is set to the concurrent resource request.
			// If the concurrent resource request is smaller than the target (e.g., use a default value not matching the actual resource limit),
			// we use the target as the total.
			target = annotationTarget
			total = float64(pa.Spec.ConcurrentResourceRequest)
			total = math.Max(total, target)
			return target, total
		} else {
			// If the target is not set in the annotation, use the concurrent resource request times the target utilization as the target.
			total = float64(pa.Spec.ConcurrentResourceRequest)
			tu = config.TargetUtilization
			if v, ok := pa.TargetUtilization(); ok {
				// If the target utilization is set in the annotation, use it.
				tu = v
			}
			target = total * tu
			return target, total
		}
	default:
		// Concurrency is used by default
		total = float64(pa.Spec.ContainerConcurrency) // Default ContainerConcurrency is 0 (no limit), the maximum limit is 1000 (imposed by validator) if set.
		// If containerConcurrency is 0 we'll always target the default.
		if total == 0 {
			total = config.ContainerConcurrencyTargetDefault // ContainerConcurrencyTargetDefault is 100
		}
		tu = config.ContainerConcurrencyTargetFraction // Default ContainerConcurrencyTargetFraction is 0.7
	}

	// Use the target provided via annotation, if applicable.
	if annotationTarget, ok := pa.Target(); ok {
		total = annotationTarget
		if pa.Metric() == autoscaling.Concurrency && pa.Spec.ContainerConcurrency != 0 {
			// We pick the smaller value between container concurrency and the annotationTarget
			// to make sure the autoscaler does not aim for a higher concurrency than the application
			// can handle per containerConcurrency.
			total = math.Min(annotationTarget, float64(pa.Spec.ContainerConcurrency))
		}
	}

	if v, ok := pa.TargetUtilization(); ok {
		// If the target utilization is set in the annotation, use it.
		tu = v
	}
	//				  TargetMin is 0.01
	target = math.Max(autoscaling.TargetMin, total*tu)

	return target, total
}
