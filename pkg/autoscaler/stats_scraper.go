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
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling/kpa/resources/names"
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

	samplingSuccessRate = 0.8
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

// sampleClient defines the interface for getting a sample size for a given
// population. Internal used only.
type sampleClient interface {
	// SampleSize returns the sample size of the given population.
	SampleSize(population int) int
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
	sClient             scrapeClient
	samClient           sampleClient
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
	return newServiceScraperWithClient(metric, endpointsLister, sClient, nil)
}

func newServiceScraperWithClient(
	metric *Metric,
	endpointsLister corev1listers.EndpointsLister,
	sClient scrapeClient,
	samClient sampleClient) (*ServiceScraper, error) {
	if metric == nil {
		return nil, errors.New("metric must not be nil")
	}
	if endpointsLister == nil {
		return nil, errors.New("endpoints lister must not be nil")
	}
	if sClient == nil {
		return nil, errors.New("scrape client must not be nil")
	}
	if samClient == nil {
		return nil, errors.New("sample client must not be nil")
	}
	revName := metric.Labels[serving.RevisionLabelKey]
	if revName == "" {
		return nil, fmt.Errorf("no Revision label found for Metric %s", metric.Name)
	}

	serviceName := names.MetricsServiceName(revName)
	return &ServiceScraper{
		sClient:             sClient,
		samClient:           samClient,
		endpointsLister:     endpointsLister,
		url:                 fmt.Sprintf("http://%s.%s:%d/metrics", serviceName, metric.Namespace, v1alpha1.RequestQueueMetricsPort),
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

	sampleSize := s.samClient.SampleSize(readyPodsCount)

	var avgCon float64
	var avgProxiedCon float64
	var reqC int32
	var proxiedReqC int32
	var sussC float64
	var errRec error
	for i := 0; i < sampleSize; i++ {
		stat, err := s.sClient.Scrape(s.url)
		if err != nil {
			errRec = err
			continue
		}

		sussC++
		avgCon += stat.AverageConcurrentRequests
		avgProxiedCon += stat.AverageProxiedConcurrentRequests
		reqC += stat.RequestCount
		proxiedReqC += stat.ProxiedRequestCount
	}

	if sussC/float64(sampleSize) < samplingSuccessRate {
		return nil, errRec
	}

	avgCon = avgCon / sussC
	avgProxiedCon = avgProxiedCon / sussC
	reqC = int32(float64(reqC) / sussC)
	proxiedReqC = int32(float64(proxiedReqC) / sussC)
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
		AverageConcurrentRequests:        avgCon * float64(readyPodsCount),
		AverageProxiedConcurrentRequests: avgProxiedCon * float64(readyPodsCount),
		RequestCount:                     reqC * int32(readyPodsCount),
		ProxiedRequestCount:              proxiedReqC * int32(readyPodsCount),
	}

	return &StatMessage{
		Stat: extrapolatedStat,
		Key:  s.metricKey,
	}, nil
}
