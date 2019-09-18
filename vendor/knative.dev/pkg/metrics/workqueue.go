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

package metrics

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"k8s.io/client-go/util/workqueue"
)

// WorkqueueProvider implements workqueue.MetricsProvider and may be used with
// workqueue.SetProvider to have metrics exported to the provided metrics.
type WorkqueueProvider struct {
	Adds         *stats.Int64Measure
	Depth        *stats.Int64Measure
	Latency      *stats.Float64Measure
	Retries      *stats.Int64Measure
	WorkDuration *stats.Float64Measure
}

var (
	_ workqueue.MetricsProvider = (*WorkqueueProvider)(nil)
)

// NewAddsMetric implements MetricsProvider
func (wp *WorkqueueProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return counterMetric{
		mutators: []tag.Mutator{tag.Insert(tagName, name)},
		measure:  wp.Adds,
	}
}

// AddsView returns a view of the Adds metric.
func (wp *WorkqueueProvider) AddsView() *view.View {
	return measureView(wp.Adds, view.Count())
}

// NewDepthMetric implements MetricsProvider
func (wp *WorkqueueProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return &gaugeMetric{
		mutators: []tag.Mutator{tag.Insert(tagName, name)},
		measure:  wp.Depth,
	}
}

// DepthView returns a view of the Depth metric.
func (wp *WorkqueueProvider) DepthView() *view.View {
	return measureView(wp.Depth, view.LastValue())
}

// NewLatencyMetric implements MetricsProvider
func (wp *WorkqueueProvider) NewLatencyMetric(name string) workqueue.SummaryMetric {
	return floatMetric{
		mutators: []tag.Mutator{tag.Insert(tagName, name)},
		measure:  wp.Latency,
	}
}

// LatencyView returns a view of the Latency metric.
func (wp *WorkqueueProvider) LatencyView() *view.View {
	return measureView(wp.Latency, view.Distribution(BucketsNBy10(1e-08, 10)...))
}

// NewRetriesMetric implements MetricsProvider
func (wp *WorkqueueProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return counterMetric{
		mutators: []tag.Mutator{tag.Insert(tagName, name)},
		measure:  wp.Retries,
	}
}

// RetriesView returns a view of the Retries metric.
func (wp *WorkqueueProvider) RetriesView() *view.View {
	return measureView(wp.Retries, view.Count())
}

// NewWorkDurationMetric implements MetricsProvider
func (wp *WorkqueueProvider) NewWorkDurationMetric(name string) workqueue.SummaryMetric {
	return floatMetric{
		mutators: []tag.Mutator{tag.Insert(tagName, name)},
		measure:  wp.WorkDuration,
	}
}

// WorkDurationView returns a view of the WorkDuration metric.
func (wp *WorkqueueProvider) WorkDurationView() *view.View {
	return measureView(wp.WorkDuration, view.Distribution(BucketsNBy10(1e-08, 10)...))
}

// DefaultViews returns a list of views suitable for passing to view.Register
func (wp *WorkqueueProvider) DefaultViews() []*view.View {
	return []*view.View{
		wp.AddsView(),
		wp.DepthView(),
		wp.LatencyView(),
		wp.RetriesView(),
		wp.WorkDurationView(),
	}
}
