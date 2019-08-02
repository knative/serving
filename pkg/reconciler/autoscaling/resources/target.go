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

	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
)

// ResolveMetricTarget takes scaling metric knobs from multiple locations
// and resolves them to the final value to be used by the autoscaler.
// `target` is the target value of scaling metric that we autoscaler will aim for;
// `total` is the maximum possible value of scaling metric that is permitted on the pod.
func ResolveMetricTarget(pa *v1alpha1.PodAutoscaler, config *autoscaler.Config) (target float64, total float64) {
	// TODO(yanweiguo): currently concurrency is the only supported metric so
	// we takes all concurrency knobs directly. Once we support other metric,
	// for example requests per second, we need to calculate the target values
	// based on which metric is used for autoscaling.
	total = float64(pa.Spec.ContainerConcurrency)
	// If containerConcurrency is 0 we'll always target the default.
	if total == 0 {
		total = config.ContainerConcurrencyTargetDefault
	}

	tu := config.ContainerConcurrencyTargetFraction
	if v, ok := pa.TargetUtilization(); ok {
		tu = v
	}
	target = math.Max(1, total*tu)

	// Use the target provided via annotation, if applicable.
	if annotationTarget, ok := pa.Target(); ok {
		// We pick the smaller value between the calculated target and the annotationTarget
		// to make sure the autoscaler does not aim for a higher concurrency than the application
		// can handle per containerConcurrency.
		target = math.Max(1, math.Min(target, annotationTarget*tu))
		total = math.Min(annotationTarget, total)
	}

	return target, total
}
