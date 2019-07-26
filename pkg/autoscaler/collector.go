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
	"errors"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/autoscaler/aggregation"

	"go.uber.org/zap"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
)

const (
	// scrapeTickInterval is the interval of time between triggering StatsScraper.Scrape()
	// to get metrics across all pods of a revision.
	scrapeTickInterval = time.Second

	// BucketSize is the size of the buckets of stats we create.
	BucketSize = 2 * time.Second
)

var (
	// ErrNoData denotes that the collector could not calculate data.
	ErrNoData = errors.New("no data available")

	// ErrNotScraping denotes that the collector is not collecting metrics for the given resource.
	ErrNotScraping = errors.New("the requested resource is not being scraped")
)

// StatsScraperFactory creates a StatsScraper for a given Metric.
type StatsScraperFactory func(*av1alpha1.Metric) (StatsScraper, error)

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
	RequestCount float64

	// Part of RequestCount, for requests going through a proxy.
	ProxiedRequestCount float64
}

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	Key  types.NamespacedName
	Stat Stat
}

// Collector starts and stops metric collection for a given entity.
type Collector interface {
	// CreateOrUpdate either creates a collection for the given metric or update it, should
	// it already exist.
	CreateOrUpdate(*av1alpha1.Metric) error
	// Record allows stats to be captured that came from outside the Collector.
	Record(key types.NamespacedName, stat Stat)
	// Delete deletes a Metric and halts collection.
	Delete(string, string) error
}

// MetricClient surfaces the metrics that can be obtained via the collector.
type MetricClient interface {
	// StableAndPanicConcurrency returns both the stable and the panic concurrency
	// for the given replica as of the given time.
	StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error)
}

// MetricCollector manages collection of metrics for many entities.
type MetricCollector struct {
	logger *zap.SugaredLogger

	statsScraperFactory StatsScraperFactory

	collections      map[types.NamespacedName]*collection
	collectionsMutex sync.RWMutex
}

var _ Collector = (*MetricCollector)(nil)
var _ MetricClient = (*MetricCollector)(nil)

// NewMetricCollector creates a new metric collector.
func NewMetricCollector(statsScraperFactory StatsScraperFactory, logger *zap.SugaredLogger) *MetricCollector {
	collector := &MetricCollector{
		logger:              logger,
		collections:         make(map[types.NamespacedName]*collection),
		statsScraperFactory: statsScraperFactory,
	}

	return collector
}

// CreateOrUpdate either creates a collection for the given metric or update it, should
// it already exist.
// Map access optimized via double-checked locking.
func (c *MetricCollector) CreateOrUpdate(metric *av1alpha1.Metric) error {
	scraper, err := c.statsScraperFactory(metric)
	if err != nil {
		return err
	}
	key := types.NamespacedName{Namespace: metric.Namespace, Name: metric.Name}

	c.collectionsMutex.RLock()
	collection, exists := c.collections[key]
	c.collectionsMutex.RUnlock()
	if exists {
		collection.updateScraper(scraper)
		collection.updateMetric(metric)
		return nil
	}

	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	collection, exists = c.collections[key]
	if exists {
		collection.updateScraper(scraper)
		collection.updateMetric(metric)
		return nil
	}

	c.collections[key] = newCollection(metric, scraper, c.logger)
	return nil
}

// Delete deletes a Metric and halts collection.
func (c *MetricCollector) Delete(namespace, name string) error {
	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	c.logger.Debugf("Stopping metric collection of %s/%s", namespace, name)

	key := types.NamespacedName{Namespace: namespace, Name: name}
	if collection, ok := c.collections[key]; ok {
		collection.close()
		delete(c.collections, key)
	}
	return nil
}

// Record records a stat that's been generated outside of the metric collector.
func (c *MetricCollector) Record(key types.NamespacedName, stat Stat) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	if collection, exists := c.collections[key]; exists {
		collection.record(stat)
	}
}

// StableAndPanicConcurrency returns both the stable and the panic concurrency.
// It may truncate metric buckets as a side-effect.
func (c *MetricCollector) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	collection, exists := c.collections[key]
	if !exists {
		return 0, 0, ErrNotScraping
	}

	return collection.stableAndPanicConcurrency(now)
}

// collection represents the collection of metrics for one specific entity.
type collection struct {
	metricMutex sync.RWMutex
	metric      *av1alpha1.Metric

	scraperMutex sync.RWMutex
	scraper      StatsScraper
	buckets      *aggregation.TimedFloat64Buckets

	grp    sync.WaitGroup
	stopCh chan struct{}
}

func (c *collection) updateScraper(ss StatsScraper) {
	c.scraperMutex.Lock()
	defer c.scraperMutex.Unlock()
	c.scraper = ss
}

func (c *collection) getScraper() StatsScraper {
	c.scraperMutex.RLock()
	defer c.scraperMutex.RUnlock()
	return c.scraper
}

// newCollection creates a new collection, which uses the given scraper to
// collect stats every scrapeTickInterval.
func newCollection(metric *av1alpha1.Metric, scraper StatsScraper, logger *zap.SugaredLogger) *collection {
	c := &collection{
		metric:  metric,
		buckets: aggregation.NewTimedFloat64Buckets(BucketSize),
		scraper: scraper,

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
				message, err := c.getScraper().Scrape()
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
func (c *collection) updateMetric(metric *av1alpha1.Metric) {
	c.metricMutex.Lock()
	defer c.metricMutex.Unlock()

	c.metric = metric
}

// currentMetric safely returns the current metric stored in the collection.
func (c *collection) currentMetric() *av1alpha1.Metric {
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
