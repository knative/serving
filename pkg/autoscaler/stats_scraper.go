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

package autoscaler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/types"

	"github.com/pkg/errors"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/resources"
)

const (
	httpClientTimeout = 3 * time.Second

	// scraperPodName is the name used in all stats sent from the scraper to
	// the autoscaler. The actual customer pods are hidden behind the scraper. The
	// autoscaler does need to know how many customer pods are reporting metrics.
	// Instead, the autoscaler knows the stats it receives are either from the
	// scraper or the activator.
	scraperPodName = "service-scraper"

	// scraperMaxRetries are retries to be done to the actual Scrape routine. We want
	// to retry if a Scrape returns an error or if the Scrape goes to a pod we already
	// scraped.
	scraperMaxRetries = 10
)

// StatsScraper defines the interface for collecting Revision metrics
type StatsScraper interface {
	// Scrape scrapes the Revision queue metric endpoint.
	Scrape() (*StatMessage, error)
}

// scrapeClient defines the interface for collecting Revision metrics for a given
// URL. Internal used only.
type scrapeClient interface {
	// Scrape scrapes the given URL.
	Scrape(url string) (*Stat, error)
}

// cacheDisabledClient is a http client with cache disabled. It is shared by
// every goruntime for a revision scraper.
var cacheDisabledClient = &http.Client{
	Transport: &http.Transport{
		// Do not use the cached connection
		DisableKeepAlives: true,
	},
	Timeout: httpClientTimeout,
}

// ServiceScraper scrapes Revision metrics via a K8S service by sampling. Which
// pod to be picked up to serve the request is decided by K8S. Please see
// https://kubernetes.io/docs/concepts/services-networking/network-policies/
// for details.
type ServiceScraper struct {
	sClient   scrapeClient
	counter   resources.ReadyPodCounter
	namespace string
	metricKey types.NamespacedName
	url       string
}

// NewServiceScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func NewServiceScraper(metric *av1alpha1.Metric, counter resources.ReadyPodCounter) (*ServiceScraper, error) {
	sClient, err := newHTTPScrapeClient(cacheDisabledClient)
	if err != nil {
		return nil, err
	}
	return newServiceScraperWithClient(metric, counter, sClient)
}

func newServiceScraperWithClient(
	metric *av1alpha1.Metric,
	counter resources.ReadyPodCounter,
	sClient scrapeClient) (*ServiceScraper, error) {
	if metric == nil {
		return nil, errors.New("metric must not be nil")
	}
	if counter == nil {
		return nil, errors.New("counter must not be nil")
	}
	if sClient == nil {
		return nil, errors.New("scrape client must not be nil")
	}
	revName := metric.Labels[serving.RevisionLabelKey]
	if revName == "" {
		return nil, fmt.Errorf("no Revision label found for Metric %s", metric.Name)
	}

	return &ServiceScraper{
		sClient:   sClient,
		counter:   counter,
		url:       urlFromTarget(metric.Spec.ScrapeTarget, metric.ObjectMeta.Namespace),
		metricKey: types.NamespacedName{Namespace: metric.Namespace, Name: metric.Name},
		namespace: metric.Namespace,
	}, nil
}

func urlFromTarget(t, ns string) string {
	return fmt.Sprintf(
		"http://%s.%s:%d/metrics",
		t, ns, networking.AutoscalingQueueMetricsPort)
}

// Scrape calls the destination service then sends it
// to the given stats channel.
func (s *ServiceScraper) Scrape() (*StatMessage, error) {
	readyPodsCount, err := s.counter.ReadyCount()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get endpoints")
	}

	if readyPodsCount == 0 {
		return nil, nil
	}

	sampleSize := populationMeanSampleSize(readyPodsCount)
	statCh := make(chan *Stat, sampleSize)
	scrapedPods := &sync.Map{}

	grp := errgroup.Group{}
	for i := 0; i < sampleSize; i++ {
		grp.Go(func() error {
			for tries := 1; ; tries++ {
				stat, err := s.tryScrape(scrapedPods)
				if err == nil {
					statCh <- stat
					return nil
				}

				// Return the error if we exhausted our retries.
				if tries == scraperMaxRetries {
					return err
				}
			}
		})
	}

	// Return the inner error, if any.
	if err := grp.Wait(); err != nil {
		return nil, errors.Wrapf(err, "unsuccessful scrape, sampleSize=%d", sampleSize)
	}
	close(statCh)

	var (
		avgConcurrency        float64
		avgProxiedConcurrency float64
		reqCount              float64
		proxiedReqCount       float64
		successCount          float64
	)

	for stat := range statCh {
		successCount++
		avgConcurrency += stat.AverageConcurrentRequests
		avgProxiedConcurrency += stat.AverageProxiedConcurrentRequests
		reqCount += stat.RequestCount
		proxiedReqCount += stat.ProxiedRequestCount
	}

	frpc := float64(readyPodsCount)
	avgConcurrency = avgConcurrency / successCount
	avgProxiedConcurrency = avgProxiedConcurrency / successCount
	reqCount = reqCount / successCount
	proxiedReqCount = proxiedReqCount / successCount
	now := time.Now()

	// Assumption: A particular pod can stand for other pods, i.e. other pods
	// have similar concurrency and QPS.
	//
	// Hide the actual pods behind scraper and send only one stat for all the
	// customer pods per scraping. The pod name is set to a unique value, i.e.
	// scraperPodName so in autoscaler all stats are either from activator or
	// scraper.
	extrapolatedStat := Stat{
		Time:                             &now,
		PodName:                          scraperPodName,
		AverageConcurrentRequests:        avgConcurrency * frpc,
		AverageProxiedConcurrentRequests: avgProxiedConcurrency * frpc,
		RequestCount:                     reqCount * frpc,
		ProxiedRequestCount:              proxiedReqCount * frpc,
	}

	return &StatMessage{
		Stat: extrapolatedStat,
		Key:  s.metricKey,
	}, nil
}

// tryScrape runs a single scrape and checks if this pod wasn't already scraped
// against the given already scraped pods.
func (s *ServiceScraper) tryScrape(scrapedPods *sync.Map) (*Stat, error) {
	stat, err := s.sClient.Scrape(s.url)
	if err != nil {
		return nil, err
	}

	if _, exists := scrapedPods.LoadOrStore(stat.PodName, struct{}{}); exists {
		return nil, errors.New("did not receive stat from an unscraped pod")
	}

	return stat, nil
}
