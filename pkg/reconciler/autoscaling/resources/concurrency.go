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
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
)

// ResolveTargetConcurrency takes concurrency knobs from multiple locations and resolves them
// to the final value to be used by the autoscaler.
func ResolveTargetConcurrency(pa *v1alpha1.PodAutoscaler, config *autoscaler.Config) (target float64) {
	// If containerConcurrency is 0 we'll always target the default.
	if pa.Spec.ContainerConcurrency == 0 {
		target = config.ContainerConcurrencyTargetDefault
	} else {
		// For a non-zero containerConcurrency we calculate the actual concurrency using the config.
		target = float64(pa.Spec.ContainerConcurrency) * config.ContainerConcurrencyTargetPercentage
	}

	// Use the target provided via annotation, if applicable.
	if mt, ok := pa.Target(); ok {
		annotationTarget := float64(mt)
		// If the annotation target would cause the autoscaler to maintain
		// more requests per pod than the container can handle, we ignore
		// the annotation and use a containerConcurrency based target instead.
		if annotationTarget <= target {
			target = annotationTarget
		}
	}

	return target
}
