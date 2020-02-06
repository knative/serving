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
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/resources"
)

// StableWindow returns the stable window for the revision from PA, if set, or
// systemwide default.
func StableWindow(pa *v1alpha1.PodAutoscaler, config *autoscaler.Config) time.Duration {
	sw, ok := pa.Window()
	if !ok {
		sw = config.StableWindow
	}
	return sw
}

// MakeMetric constructs a Metric resource from a PodAutoscaler
func MakeMetric(ctx context.Context, pa *v1alpha1.PodAutoscaler, metricSvc string,
	config *autoscaler.Config) *v1alpha1.Metric {
	stableWindow := StableWindow(pa, config)

	// Look for a panic window percentage annotation.
	panicWindowPercentage, ok := pa.PanicWindowPercentage()
	if !ok {
		// Fall back to cluster config.
		panicWindowPercentage = config.PanicWindowPercentage
	}
	panicWindow := time.Duration(float64(stableWindow) * panicWindowPercentage / 100.0).Round(time.Second)
	if panicWindow < autoscaler.BucketSize {
		panicWindow = autoscaler.BucketSize
	}
	return &v1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       pa.Namespace,
			Name:            pa.Name,
			Annotations:     resources.CopyMap(pa.Annotations),
			Labels:          resources.CopyMap(pa.Labels),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(pa)},
		},
		Spec: v1alpha1.MetricSpec{
			StableWindow: stableWindow,
			PanicWindow:  panicWindow,
			ScrapeTarget: metricSvc,
		},
	}
}
