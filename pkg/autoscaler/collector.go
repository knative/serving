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
	"knative.dev/pkg/logging/logkey"
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
type StatsScraperFactory func(*av1alpha1.Metric, *zap.SugaredLogger) (StatsScraper, error)

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

	// Number of requests received since last Stat (approximately requests per second).
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

	// StableAndPanicRPS returns both the stable and the panic RPS
	// for the given replica as of the given time.
	StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error)
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
	l := c.logger.With(zap.String(logkey.Key, metric.Namespace+"/"+metric.Name))
	scraper, err := c.statsScraperFactory(metric, l)
	if err != nil {
		return err
	}
	key := types.NamespacedName{Namespace: metric.Namespace, Name: metric.Name}
	c.logger.Info("Starting collection for ", key.String())

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

	c.logger.Infof("Stopping metric collection of %s/%s", namespace, name)

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

// StableAndPanicRPS returns both the stable and the panic RPS.
// It may truncate metric buckets as a side-effect.
func (c *MetricCollector) StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	collection, exists := c.collections[key]
	if !exists {
		return 0, 0, ErrNotScraping
	}

	return collection.StableAndPanicRPS(now)
}

// collection represents the collection of metrics for one specific entity.
type collection struct {
	metricMutex sync.RWMutex
	metric      *av1alpha1.Metric

	scraperMutex       sync.RWMutex
	scraper            StatsScraper
	concurrencyBuckets *aggregation.TimedFloat64Buckets
	rpsBuckets         *aggregation.TimedFloat64Buckets

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
		metric:             metric,
		concurrencyBuckets: aggregation.NewTimedFloat64Buckets(BucketSize),
		rpsBuckets:         aggregation.NewTimedFloat64Buckets(BucketSize),
		scraper:            scraper,

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
					copy := metric.DeepCopy()
					switch {
					case err == ErrFailedGetEndpoints:
						copy.Status.MarkMetricNotReady("NoEndpoints", ErrFailedGetEndpoints.Error())
					case err == ErrDidNotReceiveStat:
						copy.Status.MarkMetricFailed("DidNotReceiveStat", ErrDidNotReceiveStat.Error())
					default:
						copy.Status.MarkMetricNotReady("CreateOrUpdateFailed", "Collector has failed.")
					}
					logger.Errorw("Failed to scrape metrics", zap.Error(err))
					c.updateMetric(copy)
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
	spec := c.currentMetric().Spec

	// Proxied requests have been counted at the activator. Subtract
	// them to avoid double counting.
	c.concurrencyBuckets.Record(*stat.Time, stat.PodName, stat.AverageConcurrentRequests-stat.AverageProxiedConcurrentRequests)
	c.rpsBuckets.Record(*stat.Time, stat.PodName, stat.RequestCount-stat.ProxiedRequestCount)

	// Delete outdated stats taking stat.Time as current time.
	now := stat.Time
	c.concurrencyBuckets.RemoveOlderThan(now.Add(-spec.StableWindow))
	c.rpsBuckets.RemoveOlderThan(now.Add(-spec.StableWindow))
}

// stableAndPanicConcurrency calculates both stable and panic concurrency based on the
// current stats.
func (c *collection) stableAndPanicConcurrency(now time.Time) (float64, float64, error) {
	return c.stableAndPanicStats(now, c.concurrencyBuckets)
}

// StableAndPanicRPS calculates both stable and panic RPS based on the
// current stats.
func (c *collection) StableAndPanicRPS(now time.Time) (float64, float64, error) {
	return c.stableAndPanicStats(now, c.rpsBuckets)
}

// stableAndPanicStats calculates both stable and panic concurrency based on the
// given stats buckets.
func (c *collection) stableAndPanicStats(now time.Time, buckets *aggregation.TimedFloat64Buckets) (float64, float64, error) {
	metric := c.currentMetric()
	copy := metric.DeepCopy()
	if buckets.IsEmpty() {
		copy := metric.DeepCopy()
		copy.Status.MarkMetricNotReady("NoData", ErrNoData.Error())
		c.updateMetric(copy)
		return 0, 0, ErrNoData
	}
	spec := metric.Spec

	panicAverage := aggregation.Average{}
	stableAverage := aggregation.Average{}
	buckets.ForEachBucket(
		aggregation.YoungerThan(now.Add(-spec.PanicWindow), panicAverage.Accumulate),
		aggregation.YoungerThan(now.Add(-spec.StableWindow), stableAverage.Accumulate),
	)

	copy.Status.MarkMetricReady()
	c.updateMetric(copy)
	return stableAverage.Value(), panicAverage.Value(), nil
}

// close stops collecting metrics, stops the scraper.
func (c *collection) close() {
	close(c.stopCh)
	c.grp.Wait()
}
