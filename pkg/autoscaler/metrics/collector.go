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
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"knative.dev/pkg/logging/logkey"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/aggregation"
	"knative.dev/serving/pkg/autoscaler/config"
)

const (
	// scrapeTickInterval is the interval of time between triggering StatsScraper.Scrape()
	// to get metrics across all pods of a revision.
	scrapeTickInterval = time.Second
)

var (
	// ErrNoData denotes that the collector could not calculate data.
	ErrNoData = errors.New("no data available")

	// ErrNotCollecting denotes that the collector is not collecting metrics for the given resource.
	ErrNotCollecting = errors.New("no metrics are being collected for the requested resource")
)

// StatsScraperFactory creates a StatsScraper for a given Metric.
type StatsScraperFactory func(*av1alpha1.Metric, *zap.SugaredLogger) (StatsScraper, error)

var emptyStat = Stat{}

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
	Record(key types.NamespacedName, now time.Time, stat Stat)
	// Delete deletes a Metric and halts collection.
	Delete(string, string) error
	// Watch registers a singleton function to call when a specific collector's status changes.
	// The passed name is the namespace/name of the metric owned by the respective collector.
	Watch(func(types.NamespacedName))
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
	clock               clock.Clock

	collectionsMutex sync.RWMutex
	collections      map[types.NamespacedName]*collection

	watcherMutex sync.RWMutex
	watcher      func(types.NamespacedName)
}

var _ Collector = (*MetricCollector)(nil)
var _ MetricClient = (*MetricCollector)(nil)

// NewMetricCollector creates a new metric collector.
func NewMetricCollector(statsScraperFactory StatsScraperFactory, logger *zap.SugaredLogger) *MetricCollector {
	return &MetricCollector{
		logger:              logger,
		collections:         make(map[types.NamespacedName]*collection),
		statsScraperFactory: statsScraperFactory,
		clock:               clock.RealClock{},
	}
}

// CreateOrUpdate either creates a collection for the given metric or update it, should
// it already exist.
func (c *MetricCollector) CreateOrUpdate(metric *av1alpha1.Metric) error {
	logger := c.logger.With(zap.String(logkey.Key, types.NamespacedName{
		Namespace: metric.Namespace,
		Name:      metric.Name,
	}.String()))
	scraper, err := c.statsScraperFactory(metric, logger)
	if err != nil {
		return err
	}
	key := types.NamespacedName{Namespace: metric.Namespace, Name: metric.Name}

	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	collection, exists := c.collections[key]
	if exists {
		collection.updateScraper(scraper)
		collection.updateMetric(metric)
		return collection.lastError()
	}

	c.collections[key] = newCollection(metric, scraper, c.clock, c.Inform, logger)
	return nil
}

// Delete deletes a Metric and halts collection.
func (c *MetricCollector) Delete(namespace, name string) error {
	c.collectionsMutex.Lock()
	defer c.collectionsMutex.Unlock()

	key := types.NamespacedName{Namespace: namespace, Name: name}
	if collection, ok := c.collections[key]; ok {
		collection.close()
		delete(c.collections, key)
	}
	return nil
}

// Record records a stat that's been generated outside of the metric collector.
func (c *MetricCollector) Record(key types.NamespacedName, now time.Time, stat Stat) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	if collection, exists := c.collections[key]; exists {
		collection.record(now, stat)
	}
}

// Watch registers a singleton function to call when collector status changes.
func (c *MetricCollector) Watch(fn func(types.NamespacedName)) {
	c.watcherMutex.Lock()
	defer c.watcherMutex.Unlock()

	if c.watcher != nil {
		c.logger.Panic("Multiple calls to Watch() not supported")
	}
	c.watcher = fn
}

// Inform sends an update to the registered watcher function, if it is set.
func (c *MetricCollector) Inform(event types.NamespacedName) {
	c.watcherMutex.RLock()
	defer c.watcherMutex.RUnlock()

	if c.watcher != nil {
		c.watcher(event)
	}
}

// StableAndPanicConcurrency returns both the stable and the panic concurrency.
// It may truncate metric buckets as a side-effect.
func (c *MetricCollector) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	collection, exists := c.collections[key]
	if !exists {
		return 0, 0, ErrNotCollecting
	}

	if collection.concurrencyBuckets.IsEmpty(now) && collection.currentMetric().Spec.ScrapeTarget != "" {
		return 0, 0, ErrNoData
	}
	return collection.concurrencyBuckets.WindowAverage(now),
		collection.concurrencyPanicBuckets.WindowAverage(now),
		nil
}

// StableAndPanicRPS returns both the stable and the panic RPS.
// It may truncate metric buckets as a side-effect.
func (c *MetricCollector) StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	c.collectionsMutex.RLock()
	defer c.collectionsMutex.RUnlock()

	collection, exists := c.collections[key]
	if !exists {
		return 0, 0, ErrNotCollecting
	}

	if collection.rpsBuckets.IsEmpty(now) && collection.currentMetric().Spec.ScrapeTarget != "" {
		return 0, 0, ErrNoData
	}
	return collection.rpsBuckets.WindowAverage(now),
		collection.rpsPanicBuckets.WindowAverage(now),
		nil
}

// collection represents the collection of metrics for one specific entity.
type collection struct {
	// mux guards access to all of the collection's state.
	mux sync.RWMutex

	metric *av1alpha1.Metric

	// Fields relevant to metric collection in general.
	concurrencyBuckets      *aggregation.TimedFloat64Buckets
	concurrencyPanicBuckets *aggregation.TimedFloat64Buckets
	rpsBuckets              *aggregation.TimedFloat64Buckets
	rpsPanicBuckets         *aggregation.TimedFloat64Buckets

	// Fields relevant for metric scraping specifically.
	scraper StatsScraper
	lastErr error
	grp     sync.WaitGroup
	stopCh  chan struct{}
}

func (c *collection) updateScraper(ss StatsScraper) {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.scraper = ss
}

func (c *collection) getScraper() StatsScraper {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.scraper
}

// newCollection creates a new collection, which uses the given scraper to
// collect stats every scrapeTickInterval.
func newCollection(metric *av1alpha1.Metric, scraper StatsScraper, clock clock.Clock,
	callback func(types.NamespacedName), logger *zap.SugaredLogger) *collection {
	c := &collection{
		metric: metric,
		concurrencyBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.StableWindow, config.BucketSize),
		concurrencyPanicBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.PanicWindow, config.BucketSize),
		rpsBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.StableWindow, config.BucketSize),
		rpsPanicBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.PanicWindow, config.BucketSize),
		scraper: scraper,

		stopCh: make(chan struct{}),
	}

	key := types.NamespacedName{Namespace: metric.Namespace, Name: metric.Name}
	logger = logger.Named("collector").With(zap.String(logkey.Key, key.String()))

	c.grp.Add(1)
	go func() {
		defer c.grp.Done()

		scrapeTicker := clock.NewTicker(scrapeTickInterval)
		defer scrapeTicker.Stop()
		for {
			select {
			case <-c.stopCh:
				return
			case <-scrapeTicker.C():
				scraper := c.getScraper()
				if scraper == nil {
					// Don't scrape empty target service.
					if c.updateLastError(nil) {
						callback(key)
					}
					continue
				}

				stat, err := scraper.Scrape(c.currentMetric().Spec.StableWindow)
				if err != nil {
					logger.Errorw("Failed to scrape metrics", zap.Error(err))
				}
				if c.updateLastError(err) {
					callback(key)
				}
				if stat != emptyStat {
					c.record(clock.Now(), stat)
				}
			}
		}
	}()

	return c
}

// close stops collecting metrics, stops the scraper.
func (c *collection) close() {
	close(c.stopCh)
	c.grp.Wait()
}

// updateMetric safely updates the metric stored in the collection.
func (c *collection) updateMetric(metric *av1alpha1.Metric) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.metric = metric
	c.concurrencyBuckets.ResizeWindow(metric.Spec.StableWindow)
	c.concurrencyPanicBuckets.ResizeWindow(metric.Spec.PanicWindow)
	c.rpsBuckets.ResizeWindow(metric.Spec.StableWindow)
	c.rpsPanicBuckets.ResizeWindow(metric.Spec.PanicWindow)
}

// currentMetric safely returns the current metric stored in the collection.
func (c *collection) currentMetric() *av1alpha1.Metric {
	c.mux.RLock()
	defer c.mux.RUnlock()

	return c.metric
}

// updateLastError updates the last error returned from the scraper
// and returns true if the error or error state changed.
func (c *collection) updateLastError(err error) bool {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.lastErr == err {
		return false
	}
	c.lastErr = err
	return true
}

func (c *collection) lastError() error {
	c.mux.RLock()
	defer c.mux.RUnlock()

	return c.lastErr
}

// record adds a stat to the current collection.
func (c *collection) record(now time.Time, stat Stat) {
	// Proxied requests have been counted at the activator. Subtract
	// them to avoid double counting.
	concur := stat.AverageConcurrentRequests - stat.AverageProxiedConcurrentRequests
	c.concurrencyBuckets.Record(now, concur)
	c.concurrencyPanicBuckets.Record(now, concur)
	rps := stat.RequestCount - stat.ProxiedRequestCount
	c.rpsBuckets.Record(now, rps)
	c.rpsPanicBuckets.Record(now, rps)
}

// add adds the stats from `src` to `dst`.
func (dst *Stat) add(src Stat) {
	dst.AverageConcurrentRequests += src.AverageConcurrentRequests
	dst.AverageProxiedConcurrentRequests += src.AverageProxiedConcurrentRequests
	dst.RequestCount += src.RequestCount
	dst.ProxiedRequestCount += src.ProxiedRequestCount
}

// average reduces the aggregate stat from `sample` pods to an averaged one over
// `total` pods.
// The method performs no checks on the data, i.e. that sample is > 0.
//
// Assumption: A particular pod can stand for other pods, i.e. other pods
// have similar concurrency and QPS.
//
// Hide the actual pods behind scraper and send only one stat for all the
// customer pods per scraping. The pod name is set to a unique value, i.e.
// scraperPodName so in autoscaler all stats are either from activator or
// scraper.
func (dst *Stat) average(sample, total float64) {
	dst.AverageConcurrentRequests = dst.AverageConcurrentRequests / sample * total
	dst.AverageProxiedConcurrentRequests = dst.AverageProxiedConcurrentRequests / sample * total
	dst.RequestCount = dst.RequestCount / sample * total
	dst.ProxiedRequestCount = dst.ProxiedRequestCount / sample * total
}
