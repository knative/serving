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

package v1alpha1

import (
	"context"

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
)

func defaultMetric(class string) string {
	switch class {
	case autoscaling.KPA:
		return autoscaling.Concurrency
	case autoscaling.HPA:
		return autoscaling.CPU
	default:
		return ""
	}
}

// SetDefaults sets the default values for the PodAutoscaler.
func (pa *PodAutoscaler) SetDefaults(ctx context.Context) {
	pa.Spec.SetDefaults(apis.WithinSpec(ctx))
	config := config.FromContextOrDefaults(ctx)
	if pa.Annotations == nil {
		pa.Annotations = make(map[string]string, 2)
	}
	if _, ok := pa.Annotations[autoscaling.ClassAnnotationKey]; !ok {
		// Default class based on configmap setting (KPA if none specified).
		pa.Annotations[autoscaling.ClassAnnotationKey] = config.Autoscaler.PodAutoscalerClass
	}
	// Default metric per class.
	if _, ok := pa.Annotations[autoscaling.MetricAnnotationKey]; !ok {
		pa.Annotations[autoscaling.MetricAnnotationKey] = defaultMetric(pa.Class())
	}
}

// SetDefaults sets the default values for the PodAutoscalerSpec.
func (pa *PodAutoscalerSpec) SetDefaults(ctx context.Context) {}
