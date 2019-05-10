/*
Copyright 2019 The Knative Authors.

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

package autoscaler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/knative/serving/pkg/autoscaler/aggregation"

	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"go.uber.org/zap"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// scrapeTickInterval is the interval of time between scraping metrics across
	// all pods of a revision.
	// TODO(yanweiguo): tuning this value. To be based on pod population?
	scrapeTickInterval = time.Second / 3

	// bucketSize is the size of the buckets of stats we create.
	bucketSize = 2 * time.Second
)

var (
	// ErrNoData denotes that the collector could not calculate data.
	ErrNoData = errors.New("no data available")
)

// Metric represents a resource to configure the metric collector with.
// +k8s:deepcopy-gen=true
type Metric struct {
	metav1.ObjectMeta
	Spec   MetricSpec
	Status MetricStatus
}

// MetricSpec contains all values the metric collector needs to operate.
type MetricSpec struct {
	StableWindow time.Duration
	PanicWindow  time.Duration
}

// MetricStatus reflects the status of metric collection for this specific entity.
type MetricStatus struct{}

// StatsScraperFactory creates a StatsScraper for a given Metric.
type StatsScraperFactory func(*Metric) (StatsScraper, error)

// Stat defines a single measurement at a point in time
type Stat struct {
	// The time the data point was received by autoscaler.
	Time *time.Time

	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Average number of requests currently being handled by this pod.
	AverageConcurrentRequests float64

	// Part of AverageConcurrentRequests, for requests going through a proxy.
	AverageProxiedConcurrentRequests float64

	// Number of requests received since last Stat (approximately QPS).
	RequestCount int32

	// Part of RequestCount, for requests going through a proxy.
	ProxiedRequestCount int32
}

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	Key  string
	Stat Stat
}

// MetricClient surfaces the metrics that can be obtained via the collector.
type MetricClient interface {
	// StableAndPanicConcurrency returns both the stable and the panic concurrency.
	StableAndPanicConcurrency(key string) (float64, float64, error)
}

// MetricCollector manages collection of metrics for many entities.
type MetricCollector struct {
	logger *zap.SugaredLogger

	statsScraperFactory StatsScraperFactory

	collections      map[string]*collection
	collectionsMutex sync.RWMutex
}

var _ MetricClient = &MetricCollector{}

// NewMetricCollector creates a new metric collector.
func NewMetricCollector(statsScraperFactory StatsScraperFactory, logger *zap.SugaredLogger) *MetricCollector {
	collector := &MetricCollector{
		logger:              logger,
		collections:         make(map[string]*collection),
		statsScraperFactory: statsScraperFactory,
	}

	return collector
}

// Get gets a Metric's state from the collector.
// Returns a copy of the Metric object. Mutations won't be seen by the collector.
func (c *MetricCollector) Get(ctx context.Context, namespace, name string) (*Metric, error) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	key := NewMetricKey(namespace, name)
	collector, ok := c.collections[key]
	if !ok {
		return nil, k8serrors.NewNotFound(kpa.Resource("Metrics"), key)
	}

	return collector.metric.DeepCopy(), nil
}

// Create creates a new metric and thus starts collection for that entity.
// Returns a copy of the Metric object. Mutations won't be seen by the collector.
func (c *MetricCollector) Create(ctx context.Context, metric *Metric) (*Metric, error) {
	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	c.logger.Debugf("Starting metric collection of %s/%s", metric.Namespace, metric.Name)

	key := NewMetricKey(metric.Namespace, metric.Name)
	coll, exists := c.collections[key]
	if !exists {
		scraper, err := c.statsScraperFactory(metric)
		if err != nil {
			return nil, err
		}
		coll = newCollection(metric, scraper, c.logger)
		c.collections[key] = coll
	}

	return coll.metric.DeepCopy(), nil
}

// Update updates the Metric.
// Returns a copy of the Metric object. Mutations won't be seen by the collector.
func (c *MetricCollector) Update(ctx context.Context, metric *Metric) (*Metric, error) {
	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	key := NewMetricKey(metric.Namespace, metric.Name)
	if collection, exists := c.collections[key]; exists {
		collection.updateMetric(metric)
		return metric.DeepCopy(), nil
	}
	return nil, k8serrors.NewNotFound(kpa.Resource("Metrics"), key)
}

// Delete deletes a Metric and halts collection.
func (c *MetricCollector) Delete(ctx context.Context, namespace, name string) error {
	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	c.logger.Debugf("Stopping metric collection of %s/%s", namespace, name)

	key := NewMetricKey(namespace, name)
	if collection, ok := c.collections[key]; ok {
		collection.close()
		delete(c.collections, key)
	}
	return nil
}

// Record records a stat that's been generated outside of the metric collector.
func (c *MetricCollector) Record(key string, stat Stat) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	if collection, exists := c.collections[key]; exists {
		collection.record(stat)
	}
}

// StableAndPanicConcurrency returns both the stable and the panic concurrency.
func (c *MetricCollector) StableAndPanicConcurrency(key string) (float64, float64, error) {
	collection, exists := c.collections[key]
	if !exists {
		return 0, 0, k8serrors.NewNotFound(kpa.Resource("Metrics"), key)
	}

	return collection.stableAndPanicConcurrency(time.Now())
}

// collection represents the collection of metrics for one specific entity.
type collection struct {
	metricMutex sync.RWMutex
	metric      *Metric

	buckets *aggregation.TimedFloat64Buckets

	grp    sync.WaitGroup
	stopCh chan struct{}
}

// newCollection creates a new collection.
func newCollection(metric *Metric, scraper StatsScraper, logger *zap.SugaredLogger) *collection {
	c := &collection{
		metric:  metric,
		buckets: aggregation.NewTimedFloat64Buckets(bucketSize),

		stopCh: make(chan struct{}),
	}

	c.grp.Add(1)
	go func() {
		defer c.grp.Done()

		scrapeTicker := time.NewTicker(scrapeTickInterval)
		for {
			select {
			case <-c.stopCh:
				scrapeTicker.Stop()
				return
			case <-scrapeTicker.C:
				message, err := scraper.Scrape()
				if err != nil {
					logger.Errorw("Failed to scrape metrics", zap.Error(err))
				}
				if message != nil {
					c.record(message.Stat)
				}
			}
		}
	}()

	return c
}

// updateMetric safely updates the metric stored in the collection.
func (c *collection) updateMetric(metric *Metric) {
	c.metricMutex.Lock()
	defer c.metricMutex.Unlock()

	c.metric = metric
}

// currentMetric safely returns the current metric stored in the collection.
func (c *collection) currentMetric() *Metric {
	c.metricMutex.RLock()
	defer c.metricMutex.RUnlock()

	return c.metric
}

// record adds a stat to the current collection.
func (c *collection) record(stat Stat) {
	// Proxied requests have been counted at the activator. Subtract
	// AverageProxiedConcurrentRequests to avoid double counting.
	c.buckets.Record(*stat.Time, stat.PodName, stat.AverageConcurrentRequests-stat.AverageProxiedConcurrentRequests)
}

// stableAndPanicConcurrency calculates both stable and panic concurrency based on the
// current stats.
func (c *collection) stableAndPanicConcurrency(now time.Time) (float64, float64, error) {
	spec := c.currentMetric().Spec

	c.buckets.RemoveOlderThan(now.Add(-spec.StableWindow))

	if c.buckets.IsEmpty() {
		return 0, 0, ErrNoData
	}

	panicAverage := aggregation.Average{}
	stableAverage := aggregation.Average{}
	c.buckets.ForEachBucket(
		aggregation.YoungerThan(now.Add(-spec.PanicWindow), panicAverage.Accumulate),
		stableAverage.Accumulate, // No need to add a YoungerThan condition as we already deleted all outdated stats above.
	)

	return stableAverage.Value(), panicAverage.Value(), nil
}

// close stops collecting metrics, stops the scraper.
func (c *collection) close() {
	close(c.stopCh)
	c.grp.Wait()
}
