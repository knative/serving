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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"go.uber.org/zap"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	httpClientTimeout = 3 * time.Second

	cacheTimeout = time.Second

	scraperPodName = "service-scraper"
)

// StatsScraper defines the interface for collecting Revision metrics
type StatsScraper interface {
	// Scrape scrapes the Revision queue metric endpoint then sends it as a
	// StatMessage to the given channel.
	Scrape(ctx context.Context, statsCh chan<- *StatMessage)
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

type readyPods struct {
	time  time.Time
	count int
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
	cachedReadyPods readyPods
}

// NewServiceScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func NewServiceScraper(metric *Metric, dynamicConfig *DynamicConfig, endpointsInformer corev1informers.EndpointsInformer) (*ServiceScraper, error) {
	return newServiceScraperWithClient(metric, dynamicConfig, endpointsInformer, cacheDisabledClient)
}

func newServiceScraperWithClient(
	metric *Metric,
	dynamicConfig *DynamicConfig,
	endpointsInformer corev1informers.EndpointsInformer,
	httpClient *http.Client) (*ServiceScraper, error) {
	if metric == nil {
		return nil, errors.New("empty point of Metric")
	}
	if dynamicConfig == nil {
		return nil, errors.New("empty point of DynamicConfig")
	}
	if endpointsInformer == nil {
		return nil, errors.New("empty interface of EndpointsInformer")
	}
	if httpClient == nil {
		return nil, errors.New("empty point of HTTP client")
	}
	revName := metric.Labels[serving.RevisionLabelKey]
	if revName == "" {
		return nil, fmt.Errorf("no Revision label found for Metric %s", metric.Name)
	}

	serviceName := reconciler.GetServingK8SServiceNameForObj(revName)
	return &ServiceScraper{
		httpClient:      httpClient,
		endpointsLister: endpointsInformer.Lister(),
		url:             fmt.Sprintf("http://%s.%s:%d/metrics", serviceName, metric.Namespace, v1alpha1.RequestQueueMetricsPort),
		metricKey:       NewMetricKey(metric.Namespace, metric.Name),
		namespace:       metric.Namespace,
		revisionService: reconciler.GetServingK8SServiceNameForObj(revName),
		cachedReadyPods: readyPods{},
	}, nil
}

// Scrape call the destination service then send it
// to the given stats chanel
func (s *ServiceScraper) Scrape(ctx context.Context, statsCh chan<- *StatMessage) {
	logger := logging.FromContext(ctx)

	readyPodsCount, err := s.readyPodsCount(time.Now())
	if err != nil {
		logger.Errorw("Failed to get Endpoints via K8S Lister", zap.Error(err))
		return
	}

	if readyPodsCount == 0 {
		logger.Debug("No ready pods found, not to scrape.")
		return
	}

	stat, err := s.scrapeViaURL()
	if err != nil {
		logger.Errorw("Failed to get metrics", zap.Error(err))
		return
	}

	// Assume traffic is route to pods evenly. A particular pod can stand for
	// other pods, i.e. other pods have similar concurrency and QPS.
	// Hide the actual pods behind scraper and send only one stat for all the
	// customer pods per scraping. The pod name is set to a unique value, i.e.
	// scraperPodName so in autoscaler all stats are either from activator or
	// scraper.
	newStat := Stat{
		Time:                      stat.Time,
		PodName:                   scraperPodName,
		AverageConcurrentRequests: stat.AverageConcurrentRequests * float64(readyPodsCount),
		RequestCount:              stat.RequestCount * int32(readyPodsCount),
	}

	s.sendStatMessage(newStat, statsCh)
}

// readyPodsCount returns the ready IP count in the K8S Endpoints object for a Revision
// via K8S Informer. This is same as ready Pod count.
func (s *ServiceScraper) readyPodsCount(now time.Time) (int, error) {
	if s.cachedReadyPods.time.Add(cacheTimeout).After(now) {
		return s.cachedReadyPods.count, nil
	}

	readyPods, err := readyPodsCountOfEndpoints(s.endpointsLister, s.namespace, s.revisionService)
	if err != nil {
		return 0, err
	}

	s.cachedReadyPods.time = now
	s.cachedReadyPods.count = readyPods

	return readyPods, nil
}

func (s *ServiceScraper) scrapeViaURL() (*Stat, error) {
	req, err := http.NewRequest("GET", s.url, nil)
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
		return nil, fmt.Errorf("Reading text format failed: %v", err)
	}

	now := time.Now()
	stat := Stat{
		Time: &now,
	}

	if pMetric := getPrometheusMetric(metricFamilies, "queue_average_concurrent_requests"); pMetric != nil {
		stat.AverageConcurrentRequests = *pMetric.Gauge.Value
	} else {
		return nil, errors.New("Could not find value for queue_average_concurrent_requests in response")
	}

	if pMetric := getPrometheusMetric(metricFamilies, "queue_operations_per_second"); pMetric != nil {
		stat.RequestCount = int32(*pMetric.Gauge.Value)
	} else {
		return nil, errors.New("Could not find value for queue_operations_per_second in response")
	}

	return &stat, nil
}

// getPrometheusMetric returns the point of the first Metric of the MetricFamily
// with the given key from the given map. If there is no such MetricFamily or it
// has no Metrics, then returns nil.
func getPrometheusMetric(metricFamilies map[string]*dto.MetricFamily, key string) *dto.Metric {
	if metric, ok := metricFamilies[key]; ok && metric != nil && len(metric.Metric) != 0 {
		return metric.Metric[0]
	}

	return nil
}

func (s *ServiceScraper) sendStatMessage(stat Stat, statsCh chan<- *StatMessage) {
	sm := &StatMessage{
		Stat: stat,
		Key:  s.metricKey,
	}
	statsCh <- sm
}
