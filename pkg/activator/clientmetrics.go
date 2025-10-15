/*
Copyright 2025 The Knative Authors

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

package activator

import (
	"context"
	"log"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	kubecache "k8s.io/client-go/tools/cache"
	"knative.dev/pkg/metrics"
)

// reflectorCounterMetric implements kubecache.CounterMetric
type reflectorCounterMetric struct {
	measure *stats.Int64Measure
	name    string
}

func (m *reflectorCounterMetric) Inc() {
	ctx, err := tag.New(context.Background(), tag.Insert(tag.MustNewKey("name"), m.name))
	if err != nil {
		log.Printf("Failed to create tag context for reflector counter: %v", err)
		ctx = context.Background()
	}
	metrics.RecordBatch(ctx, m.measure.M(1))
}

// reflectorSummaryMetric implements kubecache.SummaryMetric
type reflectorSummaryMetric struct {
	measure *stats.Float64Measure
	name    string
}

func (m *reflectorSummaryMetric) Observe(value float64) {
	ctx, err := tag.New(context.Background(), tag.Insert(tag.MustNewKey("name"), m.name))
	if err != nil {
		log.Printf("Failed to create tag context for reflector summary: %v", err)
		ctx = context.Background()
	}
	metrics.RecordBatch(ctx, m.measure.M(value))
}

// reflectorGaugeMetric implements kubecache.GaugeMetric
type reflectorGaugeMetric struct {
	measure *stats.Float64Measure
	name    string
}

func (m *reflectorGaugeMetric) Set(value float64) {
	ctx, err := tag.New(context.Background(), tag.Insert(tag.MustNewKey("name"), m.name))
	if err != nil {
		log.Printf("Failed to create tag context for reflector gauge: %v", err)
		ctx = context.Background()
	}
	metrics.RecordBatch(ctx, m.measure.M(value))
}

// reflectorMetricsProvider implements kubecache.MetricsProvider for reflector metrics
type reflectorMetricsProvider struct {
	listsTotal          *stats.Int64Measure
	listDuration        *stats.Float64Measure
	itemsPerList        *stats.Float64Measure
	watchesTotal        *stats.Int64Measure
	shortWatchesTotal   *stats.Int64Measure
	watchDuration       *stats.Float64Measure
	itemsPerWatch       *stats.Float64Measure
	lastResourceVersion *stats.Float64Measure
}

var _ kubecache.MetricsProvider = (*reflectorMetricsProvider)(nil)

func (rp *reflectorMetricsProvider) NewListsMetric(name string) kubecache.CounterMetric {
	return &reflectorCounterMetric{
		measure: rp.listsTotal,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewListDurationMetric(name string) kubecache.SummaryMetric {
	return &reflectorSummaryMetric{
		measure: rp.listDuration,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewItemsInListMetric(name string) kubecache.SummaryMetric {
	return &reflectorSummaryMetric{
		measure: rp.itemsPerList,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewWatchesMetric(name string) kubecache.CounterMetric {
	return &reflectorCounterMetric{
		measure: rp.watchesTotal,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewShortWatchesMetric(name string) kubecache.CounterMetric {
	return &reflectorCounterMetric{
		measure: rp.shortWatchesTotal,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewWatchDurationMetric(name string) kubecache.SummaryMetric {
	return &reflectorSummaryMetric{
		measure: rp.watchDuration,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewItemsInWatchMetric(name string) kubecache.SummaryMetric {
	return &reflectorSummaryMetric{
		measure: rp.itemsPerWatch,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) NewLastResourceVersionMetric(name string) kubecache.GaugeMetric {
	return &reflectorGaugeMetric{
		measure: rp.lastResourceVersion,
		name:    name,
	}
}

func (rp *reflectorMetricsProvider) DefaultViews() []*view.View {
	nameKey := tag.MustNewKey("name")
	return []*view.View{
		{
			Description: "Total number of API lists done by reflectors",
			Measure:     rp.listsTotal,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Duration of a Kubernetes API list call in seconds",
			Measure:     rp.listDuration,
			Aggregation: view.Distribution(metrics.BucketsNBy10(1e-08, 12)...),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Number of items obtained per list",
			Measure:     rp.itemsPerList,
			Aggregation: view.Distribution(metrics.BucketsNBy10(1, 8)...),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Total number of API watches done by reflectors",
			Measure:     rp.watchesTotal,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Total number of short API watches done by reflectors",
			Measure:     rp.shortWatchesTotal,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Duration of a Kubernetes API watch call in seconds",
			Measure:     rp.watchDuration,
			Aggregation: view.Distribution(metrics.BucketsNBy10(1e-08, 12)...),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Number of items obtained per watch",
			Measure:     rp.itemsPerWatch,
			Aggregation: view.Distribution(metrics.BucketsNBy10(1, 12)...),
			TagKeys:     []tag.Key{nameKey},
		},
		{
			Description: "Last resource version seen by the reflector",
			Measure:     rp.lastResourceVersion,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{nameKey},
		},
	}
}

// SetupClientMetrics initializes client-go metrics for kubernetes API calls.
// Note: workqueue and client metrics are already set up by knative.dev/pkg/controller
// in its init() function, so we only need to set up reflector metrics here.
func SetupClientMetrics(ctx context.Context) {
	// Set up reflector metrics provider
	rp := &reflectorMetricsProvider{
		listsTotal: stats.Int64(
			"reflector_lists_total",
			"Total number of API lists done by reflectors",
			stats.UnitDimensionless,
		),
		listDuration: stats.Float64(
			"reflector_list_duration_seconds",
			"Duration of a Kubernetes API list call in seconds",
			stats.UnitSeconds,
		),
		itemsPerList: stats.Float64(
			"reflector_items_per_list",
			"Number of items obtained per list",
			stats.UnitDimensionless,
		),
		watchesTotal: stats.Int64(
			"reflector_watches_total",
			"Total number of API watches done by reflectors",
			stats.UnitDimensionless,
		),
		shortWatchesTotal: stats.Int64(
			"reflector_short_watches_total",
			"Total number of short API watches done by reflectors",
			stats.UnitDimensionless,
		),
		watchDuration: stats.Float64(
			"reflector_watch_duration_seconds",
			"Duration of a Kubernetes API watch call in seconds",
			stats.UnitSeconds,
		),
		itemsPerWatch: stats.Float64(
			"reflector_items_per_watch",
			"Number of items obtained per watch",
			stats.UnitDimensionless,
		),
		lastResourceVersion: stats.Float64(
			"reflector_last_resource_version",
			"Last resource version seen by the reflector",
			stats.UnitDimensionless,
		),
	}
	kubecache.SetReflectorMetricsProvider(rp)

	// Register views for OpenCensus
	if err := metrics.RegisterResourceView(rp.DefaultViews()...); err != nil {
		log.Fatal("Failed to register reflector metrics views: ", err)
	}

	log.Print("Registered client-go reflector metrics")
}
