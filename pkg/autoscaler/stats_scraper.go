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

	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/reconciler/autoscaling/kpa/resources/names"
	"github.com/knative/serving/pkg/resources"
	"github.com/pkg/errors"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	httpClientTimeout = 3 * time.Second

	// scraperPodName is the name used in all stats sent from the scraper to
	// the autoscaler. The actual customer pods are hidden behind the scraper. The
	// autoscaler does need to know how many customer pods are reporting metrics.
	// Instead, the autoscaler knows the stats it receives are either from the
	// scraper or the activator.
	scraperPodName = "service-scraper"
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

// SampleSizeFunc is the function for getting a sample size for a given
// population.
type SampleSizeFunc func(int) int

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
	sClient             scrapeClient
	sampleSizeFunc      SampleSizeFunc
	endpointsLister     corev1listers.EndpointsLister
	url                 string
	namespace           string
	scrapeTargetService string
	metricKey           string
}

// NewServiceScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func NewServiceScraper(metric *Metric, endpointsLister corev1listers.EndpointsLister) (*ServiceScraper, error) {
	sClient, err := newHTTPScrapeClient(cacheDisabledClient)
	if err != nil {
		return nil, err
	}
	return newServiceScraperWithClient(metric, endpointsLister, sClient)
}

func newServiceScraperWithClient(
	metric *Metric,
	endpointsLister corev1listers.EndpointsLister,
	sClient scrapeClient) (*ServiceScraper, error) {
	if metric == nil {
		return nil, errors.New("metric must not be nil")
	}
	if endpointsLister == nil {
		return nil, errors.New("endpoints lister must not be nil")
	}
	if sClient == nil {
		return nil, errors.New("scrape client must not be nil")
	}
	revName := metric.Labels[serving.RevisionLabelKey]
	if revName == "" {
		return nil, fmt.Errorf("no Revision label found for Metric %s", metric.Name)
	}

	serviceName := names.MetricsServiceName(revName)
	return &ServiceScraper{
		sClient:             sClient,
		endpointsLister:     endpointsLister,
		url:                 fmt.Sprintf("http://%s.%s:%d/metrics", serviceName, metric.Namespace, networking.RequestQueueMetricsPort),
		metricKey:           NewMetricKey(metric.Namespace, metric.Name),
		namespace:           metric.Namespace,
		scrapeTargetService: serviceName,
	}, nil
}

// Scrape calls the destination service then sends it
// to the given stats channel.
func (s *ServiceScraper) Scrape() (*StatMessage, error) {
	readyPodsCount, err := resources.FetchReadyAddressCount(s.endpointsLister, s.namespace, s.scrapeTargetService)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get endpoints")
	}

	if readyPodsCount == 0 {
		return nil, nil
	}

	sampleSize := populationMeanSampleSize(readyPodsCount)
	statCh := make(chan *Stat, sampleSize)
	defer close(statCh)

	var (
		avgConcurrency        float64
		avgProxiedConcurrency float64
		reqCount              int32
		proxiedReqCount       int32
		successCount          float64

		waitGroup sync.WaitGroup
	)

	waitGroup.Add(sampleSize)
	for i := 0; i < sampleSize; i++ {
		go func() {
			defer waitGroup.Done()

			stat, err := s.sClient.Scrape(s.url)
			if err != nil {
				statCh <- nil
			} else {
				statCh <- stat
			}
		}()
	}

	waitGroup.Wait()
	for i := 0; i < sampleSize; i++ {
		stat := <-statCh
		if stat != nil {
			successCount++
			avgConcurrency += stat.AverageConcurrentRequests
			avgProxiedConcurrency += stat.AverageProxiedConcurrentRequests
			reqCount += stat.RequestCount
			proxiedReqCount += stat.ProxiedRequestCount
		}
	}
	if successCount == 0 {
		return nil, fmt.Errorf("fail to get a successful scrape for %v tries", sampleSize)
	}

	avgConcurrency = avgConcurrency / successCount
	avgProxiedConcurrency = avgProxiedConcurrency / successCount
	reqCount = int32(float64(reqCount) / successCount)
	proxiedReqCount = int32(float64(proxiedReqCount) / successCount)
	now := time.Now()

	// Assumptions:
	// 1. Traffic is routed to pods evenly.
	// 2. A particular pod can stand for other pods, i.e. other pods have
	//    similar concurrency and QPS.
	//
	// Hide the actual pods behind scraper and send only one stat for all the
	// customer pods per scraping. The pod name is set to a unique value, i.e.
	// scraperPodName so in autoscaler all stats are either from activator or
	// scraper.
	extrapolatedStat := Stat{
		Time:                             &now,
		PodName:                          scraperPodName,
		AverageConcurrentRequests:        avgConcurrency * float64(readyPodsCount),
		AverageProxiedConcurrentRequests: avgProxiedConcurrency * float64(readyPodsCount),
		RequestCount:                     reqCount * int32(readyPodsCount),
		ProxiedRequestCount:              proxiedReqCount * int32(readyPodsCount),
	}

	return &StatMessage{
		Stat: extrapolatedStat,
		Key:  s.metricKey,
	}, nil
}
