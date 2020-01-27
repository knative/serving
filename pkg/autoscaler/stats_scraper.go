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
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

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

var (
	// Pod older than 1 minute is considered old enough for its
	// stat to be included in the sample immediately.
	// This number is arbitrary decided to be equal to the default
	// stable window.
	youngPodCutOffSecs = time.Minute.Seconds()

	// ErrFailedGetEndpoints specifies the error returned by scraper when it fails to
	// get endpoints.
	ErrFailedGetEndpoints = errors.New("failed to get endpoints")

	// ErrDidNotReceiveStat specifies the error returned by scraper when it does not receive
	// stat from an unscraped pod
	ErrDidNotReceiveStat = errors.New("did not receive stat from an unscraped pod")
)

// StatsScraper defines the interface for collecting Revision metrics
type StatsScraper interface {
	// Scrape scrapes the Revision queue metric endpoint.
	Scrape() (Stat, error)
}

// scrapeClient defines the interface for collecting Revision metrics for a given
// URL. Internal used only.
type scrapeClient interface {
	// Scrape scrapes the given URL.
	Scrape(url string) (Stat, error)
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
	sClient scrapeClient
	counter resources.EndpointsCounter
	url     string
}

// NewServiceScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func NewServiceScraper(metric *av1alpha1.Metric, counter resources.EndpointsCounter) (*ServiceScraper, error) {
	sClient, err := newHTTPScrapeClient(cacheDisabledClient)
	if err != nil {
		return nil, err
	}
	return newServiceScraperWithClient(metric, counter, sClient)
}

func newServiceScraperWithClient(
	metric *av1alpha1.Metric,
	counter resources.EndpointsCounter,
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
		sClient: sClient,
		counter: counter,
		url:     urlFromTarget(metric.Spec.ScrapeTarget, metric.ObjectMeta.Namespace),
	}, nil
}

func urlFromTarget(t, ns string) string {
	return fmt.Sprintf(
		"http://%s.%s:%d/metrics",
		t, ns, networking.AutoscalingQueueMetricsPort)
}

// Scrape calls the destination service then sends it
// to the given stats channel.
func (s *ServiceScraper) Scrape() (Stat, error) {
	readyPodsCount, err := s.counter.ReadyCount()
	if err != nil {
		return emptyStat, ErrFailedGetEndpoints
	}

	if readyPodsCount == 0 {
		return emptyStat, nil
	}

	sampleSize := populationMeanSampleSize(readyPodsCount)
	oldStatCh := make(chan Stat, sampleSize)
	youngStatCh := make(chan Stat, sampleSize)
	scrapedPods := &sync.Map{}

	grp := errgroup.Group{}
	for i := 0; i < sampleSize; i++ {
		grp.Go(func() error {
			for tries := 1; ; tries++ {
				stat, err := s.tryScrape(scrapedPods)
				if err == nil {
					if stat.ProcessUptime >= youngPodCutOffSecs {
						// We run |sampleSize| go routies and each of them terminates
						// as soon as it sees stat from an `oldPod`.
						// The channel is allocated to |sampleSize|, thus this will never
						// deadlock.
						oldStatCh <- stat
						return nil
					} else {
						select {
						// This in theory might loop over all the possible pods, thus might
						// fill up the channel.
						case youngStatCh <- stat:
						default:
							// If so, just return.
							return nil
						}
					}
				} else if tries >= scraperMaxRetries {
					// Return the error if we exhausted our retries and
					// we had an error returned (we can end up here if
					// all the pods were young, which is not an error condition).
					return err
				}
			}
			// Being here means we have effectively seen only young pods.
			return nil
		})
	}
	// Now at this point we have two possibilities.
	// 1. We scraped |sampleSize| distinct pods, with the invariant of
	// 		   sampleSize <= len(oldStatCh) + len(youngStatCh) <= sampleSize*2.
	//    Note, that `err` might still be non-nil, especially when the overall
	//    pod population is small.
	//    Consider the following case: sampleSize=3, in theory the first go routine
	//    might scrape 2 pods, the second 1 and the third won't be be able to scrape
	//		any unseen pod, so it will return `ErrDidNotReceiveStat`.
	// 2. We did not: in this case `err` below will be non-nil.

	// Return the inner error, if any.
	if err := grp.Wait(); err != nil {
		// Ignore the error if we have received enough statistics.
		if err != ErrDidNotReceiveStat || len(oldStatCh)+len(youngStatCh) < sampleSize {
			return emptyStat, fmt.Errorf("unsuccessful scrape, sampleSize=%d: %w", sampleSize, err)
		}
	}
	close(oldStatCh)
	close(youngStatCh)

	var (
		avgConcurrency        float64
		avgProxiedConcurrency float64
		reqCount              float64
		proxiedReqCount       float64
	)

	oldCnt := len(oldStatCh)
	for stat := range oldStatCh {
		avgConcurrency += stat.AverageConcurrentRequests
		avgProxiedConcurrency += stat.AverageProxiedConcurrentRequests
		reqCount += stat.RequestCount
		proxiedReqCount += stat.ProxiedRequestCount
	}
	if oldCnt < sampleSize {
		for i := oldCnt; i < sampleSize; i++ {
			// This will always succeed, see reasoning above.
			stat := <-youngStatCh
			avgConcurrency += stat.AverageConcurrentRequests
			avgProxiedConcurrency += stat.AverageProxiedConcurrentRequests
			reqCount += stat.RequestCount
			proxiedReqCount += stat.ProxiedRequestCount
		}
	}

	count := float64(sampleSize)
	frpc := float64(readyPodsCount)
	avgConcurrency = avgConcurrency / count
	avgProxiedConcurrency = avgProxiedConcurrency / count
	reqCount = reqCount / count
	proxiedReqCount = proxiedReqCount / count

	// Assumption: A particular pod can stand for other pods, i.e. other pods
	// have similar concurrency and QPS.
	//
	// Hide the actual pods behind scraper and send only one stat for all the
	// customer pods per scraping. The pod name is set to a unique value, i.e.
	// scraperPodName so in autoscaler all stats are either from activator or
	// scraper.
	return Stat{
		Time:                             time.Now(),
		PodName:                          scraperPodName,
		AverageConcurrentRequests:        avgConcurrency * frpc,
		AverageProxiedConcurrentRequests: avgProxiedConcurrency * frpc,
		RequestCount:                     reqCount * frpc,
		ProxiedRequestCount:              proxiedReqCount * frpc,
	}, nil
}

// tryScrape runs a single scrape and returns stat if this is a pod that has not been
// seen before. An error otherwise or if scraping failed.
func (s *ServiceScraper) tryScrape(scrapedPods *sync.Map) (Stat, error) {
	stat, err := s.sClient.Scrape(s.url)
	if err != nil {
		return emptyStat, err
	}

	if _, exists := scrapedPods.LoadOrStore(stat.PodName, struct{}{}); exists {
		return emptyStat, ErrDidNotReceiveStat
	}

	return stat, nil
}
