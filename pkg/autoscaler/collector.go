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
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging/logkey"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/aggregation"
)

const (
	// scrapeTickInterval is the interval of time between triggering StatsScraper.Scrape()
	// to get metrics across all pods of a revision.
	scrapeTickInterval = time.Second

	// BucketSize is the size of the buckets of stats we create.
	// NB: if this is more than 1s, we need to average values in the
	// metrics buckets.
	BucketSize = scrapeTickInterval
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
	Time time.Time

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

	// Process uptime in seconds.
	ProcessUptime float64
}

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
	tickProvider        func(time.Duration) *time.Ticker

	collections      map[types.NamespacedName]*collection
	collectionsMutex sync.RWMutex
}

var _ Collector = (*MetricCollector)(nil)
var _ MetricClient = (*MetricCollector)(nil)

// NewMetricCollector creates a new metric collector.
func NewMetricCollector(statsScraperFactory StatsScraperFactory, logger *zap.SugaredLogger) *MetricCollector {
	return &MetricCollector{
		logger:              logger,
		collections:         make(map[types.NamespacedName]*collection),
		statsScraperFactory: statsScraperFactory,
		tickProvider:        time.NewTicker,
	}
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

	c.collections[key] = newCollection(metric, scraper, c.tickProvider, c.logger)
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

	s, p, noData := collection.stableAndPanicConcurrency(now)
	if noData {
		return 0, 0, ErrNoData
	}
	return s, p, nil
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

	s, p, noData := collection.stableAndPanicRPS(now)
	if noData {
		return 0, 0, ErrNoData
	}
	return s, p, nil
}

// collection represents the collection of metrics for one specific entity.
type collection struct {
	metricMutex sync.RWMutex
	metric      *av1alpha1.Metric

	scraperMutex            sync.RWMutex
	scraper                 StatsScraper
	concurrencyBuckets      *aggregation.TimedFloat64Buckets
	concurrencyPanicBuckets *aggregation.TimedFloat64Buckets
	rpsBuckets              *aggregation.TimedFloat64Buckets
	rpsPanicBuckets         *aggregation.TimedFloat64Buckets

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
func newCollection(metric *av1alpha1.Metric, scraper StatsScraper, tickFactory func(time.Duration) *time.Ticker, logger *zap.SugaredLogger) *collection {
	c := &collection{
		metric: metric,
		concurrencyBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.StableWindow, BucketSize),
		concurrencyPanicBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.PanicWindow, BucketSize),
		rpsBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.StableWindow, BucketSize),
		rpsPanicBuckets: aggregation.NewTimedFloat64Buckets(
			metric.Spec.PanicWindow, BucketSize),
		scraper: scraper,

		stopCh: make(chan struct{}),
	}

	logger = logger.Named("collector").With(
		zap.String(logkey.Key, fmt.Sprintf("%s/%s", metric.Namespace, metric.Name)))

	c.grp.Add(1)
	go func() {
		defer c.grp.Done()

		scrapeTicker := tickFactory(scrapeTickInterval)
		for {
			select {
			case <-c.stopCh:
				scrapeTicker.Stop()
				return
			case <-scrapeTicker.C:
				stat, err := c.getScraper().Scrape()
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
				if stat != emptyStat {
					c.record(stat)
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
	c.concurrencyBuckets.ResizeWindow(metric.Spec.StableWindow)
	c.concurrencyPanicBuckets.ResizeWindow(metric.Spec.PanicWindow)
	c.rpsBuckets.ResizeWindow(metric.Spec.StableWindow)
	c.rpsPanicBuckets.ResizeWindow(metric.Spec.PanicWindow)
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
	// them to avoid double counting.
	concurr := stat.AverageConcurrentRequests - stat.AverageProxiedConcurrentRequests
	c.concurrencyBuckets.Record(stat.Time, concurr)
	c.concurrencyPanicBuckets.Record(stat.Time, concurr)
	rps := stat.RequestCount - stat.ProxiedRequestCount
	c.rpsBuckets.Record(stat.Time, rps)
	c.rpsPanicBuckets.Record(stat.Time, rps)
}

// stableAndPanicConcurrency calculates both stable and panic concurrency based on the
// current stats.
func (c *collection) stableAndPanicConcurrency(now time.Time) (float64, float64, bool) {
	return c.concurrencyBuckets.WindowAverage(now),
		c.concurrencyPanicBuckets.WindowAverage(now),
		c.concurrencyBuckets.IsEmpty(now)
}

// stableAndPanicRPS calculates both stable and panic RPS based on the
// current stats.
func (c *collection) stableAndPanicRPS(now time.Time) (float64, float64, bool) {
	return c.rpsBuckets.WindowAverage(now), c.rpsPanicBuckets.WindowAverage(now),
		c.rpsBuckets.IsEmpty(now)
}

// close stops collecting metrics, stops the scraper.
func (c *collection) close() {
	close(c.stopCh)
	c.grp.Wait()
}
