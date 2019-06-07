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

	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
)

// Metrics is an interface for notifying the presence or absence of metric collection.
type Metrics interface {
	// Get accesses the Metric resource for this key, returning any errors.
	Get(ctx context.Context, namespace, name string) (*autoscaler.Metric, error)

	// Create adds a Metric resource for a given key, returning any errors.
	Create(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error)

	// Delete removes the Metric resource for a given key, returning any errors.
	Delete(ctx context.Context, namespace, name string) error

	// Update update the Metric resource, return the new Metric or any errors.
	Update(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error)
}

// MakeMetric constructs a Metric resource from a PodAutoscaler
func MakeMetric(ctx context.Context, pa *v1alpha1.PodAutoscaler, metricSvc string,
	config *autoscaler.Config) *autoscaler.Metric {
	stableWindow, ok := pa.Window()
	if !ok {
		stableWindow = config.StableWindow
	}
	// Look for a panic window percentage annotation.
	panicWindowPercentage, ok := pa.PanicWindowPercentage()
	if !ok {
		// Fall back on cluster config.
		panicWindowPercentage = config.PanicWindowPercentage
	}
	panicWindow := time.Duration(float64(stableWindow) * panicWindowPercentage / 100.0)
	return &autoscaler.Metric{
		ObjectMeta: pa.ObjectMeta,
		Spec: autoscaler.MetricSpec{
			StableWindow: stableWindow,
			PanicWindow:  panicWindow,
			ScrapeTarget: metricSvc,
		},
	}
}
