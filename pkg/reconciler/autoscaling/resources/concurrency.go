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

	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
)

// ResolveConcurrency takes concurrency knobs from multiple locations and resolves them
// to the final value to be used by the autoscaler.
func ResolveConcurrency(pa *v1alpha1.PodAutoscaler, config *autoscaler.Config) float64 {
	target := math.Max(1, float64(pa.Spec.ContainerConcurrency)*config.ContainerConcurrencyTargetFraction)

	// If containerConcurrency is 0 we'll always target the default.
	if pa.Spec.ContainerConcurrency == 0 {
		target = config.ContainerConcurrencyTargetDefault
	}

	// Use the target provided via annotation, if applicable.
	if annotationTarget, ok := pa.Target(); ok {
		// We pick the smaller value between the calculated target and the annotationTarget
		// to make sure the autoscaler does not aim for a higher concurrency than the application
		// can handle per containerConcurrency.
		target = math.Min(target, annotationTarget)
	}

	return target
}
