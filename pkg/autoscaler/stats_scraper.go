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
	"io"
	"net/http"
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/pkg/errors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
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
	httpClient      *http.Client
	endpointsLister corev1listers.EndpointsLister
	url             string
	namespace       string
	revisionService string
	metricKey       string
}

// NewServiceScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func NewServiceScraper(decider *Decider, endpointsLister corev1listers.EndpointsLister) (*ServiceScraper, error) {
	return newServiceScraperWithClient(decider, endpointsLister, cacheDisabledClient)
}

func newServiceScraperWithClient(
	decider *Decider,
	endpointsLister corev1listers.EndpointsLister,
	httpClient *http.Client) (*ServiceScraper, error) {
	if decider == nil {
		return nil, errors.New("decider must not be nil")
	}
	if endpointsLister == nil {
		return nil, errors.New("endpoints lister must not be nil")
	}
	if httpClient == nil {
		return nil, errors.New("HTTP client must not be nil")
	}
	revName := decider.Labels[serving.RevisionLabelKey]
	if revName == "" {
		return nil, fmt.Errorf("no Revision label found for Decider %s", decider.Name)
	}

	serviceName := reconciler.GetServingK8SServiceNameForObj(revName)
	return &ServiceScraper{
		httpClient:      httpClient,
		endpointsLister: endpointsLister,
		url:             fmt.Sprintf("http://%s.%s:%d/metrics", serviceName, decider.Namespace, v1alpha1.RequestQueueMetricsPort),
		metricKey:       NewMetricKey(decider.Namespace, decider.Name),
		namespace:       decider.Namespace,
		revisionService: serviceName,
	}, nil
}

// Scrape call the destination service then send it
// to the given stats chanel
func (s *ServiceScraper) Scrape() (*StatMessage, error) {
	readyPodsCount, err := readyPodsCountOfEndpoints(s.endpointsLister, s.namespace, s.revisionService)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get endpoints")
	}

	if readyPodsCount == 0 {
		return nil, nil
	}

	stat, err := s.scrapeViaURL()
	if err != nil {
		return nil, err
	}

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
		Time:                             stat.Time,
		PodName:                          scraperPodName,
		AverageConcurrentRequests:        stat.AverageConcurrentRequests * float64(readyPodsCount),
		AverageProxiedConcurrentRequests: stat.AverageProxiedConcurrentRequests * float64(readyPodsCount),
		RequestCount:                     stat.RequestCount * int32(readyPodsCount),
		ProxiedRequestCount:              stat.ProxiedRequestCount * int32(readyPodsCount),
	}

	return &StatMessage{
		Stat: extrapolatedStat,
		Key:  s.metricKey,
	}, nil
}

func (s *ServiceScraper) scrapeViaURL() (*Stat, error) {
	req, err := http.NewRequest(http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GET request for URL %q returned HTTP status %v", s.url, resp.StatusCode)
	}

	return extractData(resp.Body)
}

func extractData(body io.Reader) (*Stat, error) {
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(body)
	if err != nil {
		return nil, fmt.Errorf("reading text format failed: %v", err)
	}

	now := time.Now()
	stat := Stat{
		Time: &now,
	}

	if pMetric := prometheusMetric(metricFamilies, "queue_average_concurrent_requests"); pMetric != nil {
		stat.AverageConcurrentRequests = *pMetric.Gauge.Value
	} else {
		return nil, errors.New("could not find value for queue_average_concurrent_requests in response")
	}

	if pMetric := prometheusMetric(metricFamilies, "queue_average_proxied_concurrent_requests"); pMetric != nil {
		stat.AverageProxiedConcurrentRequests = *pMetric.Gauge.Value
	} else {
		return nil, errors.New("could not find value for queue_average_proxied_concurrent_requests in response")
	}

	if pMetric := prometheusMetric(metricFamilies, "queue_operations_per_second"); pMetric != nil {
		stat.RequestCount = int32(*pMetric.Gauge.Value)
	} else {
		return nil, errors.New("could not find value for queue_operations_per_second in response")
	}

	if pMetric := prometheusMetric(metricFamilies, "queue_proxied_operations_per_second"); pMetric != nil {
		stat.ProxiedRequestCount = int32(*pMetric.Gauge.Value)
	} else {
		return nil, errors.New("could not find value for queue_proxied_operations_per_second in response")
	}

	return &stat, nil
}

// prometheusMetric returns the point of the first Metric of the MetricFamily
// with the given key from the given map. If there is no such MetricFamily or it
// has no Metrics, then returns nil.
func prometheusMetric(metricFamilies map[string]*dto.MetricFamily, key string) *dto.Metric {
	if metric, ok := metricFamilies[key]; ok && len(metric.Metric) > 0 {
		return metric.Metric[0]
	}

	return nil
}
