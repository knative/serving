/*
Copyright 2018 The Knative Authors

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
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/prometheus/common/expfmt"
	"go.uber.org/zap"
)

// StatsScraper defines the interface for collecting Revision metrics
type StatsScraper interface {
	// Scrape scrapes the Revision queue metric endpoint then sends it as a
	// StatMessage to the given channel.
	Scrape(statsCh chan<- *StatMessage)
}

// cacheDisabledClient is a http client with cache disabled. It is shared by
// every goruntime for a revision scraper.
var cacheDisabledClient = &http.Client{
	Transport: &http.Transport{
		// Do not use the cached connection
		DisableKeepAlives: true,
	},
	Timeout: 3 * time.Second,
}

// ServiceScraper scrapes Revision metrics via a K8S service by sampling. Which
// pod to be picked up to serve the request is decided by K8S.
type ServiceScraper struct {
	httpClient *http.Client
	url        string
	metric     *Metric
	metricKey  string
	logger     *zap.SugaredLogger
}

// CreateNewServiceScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func CreateNewServiceScraper(metric *Metric, logger *zap.SugaredLogger) (ServiceScraper, error) {
	return createServiceScraperWithClient(metric, logger, cacheDisabledClient)
}

func createServiceScraperWithClient(metric *Metric, logger *zap.SugaredLogger, httpClient *http.Client) (ServiceScraper, error) {
	revName := metric.Labels[serving.RevisionLabelKey]
	if revName == "" {
		return ServiceScraper{}, fmt.Errorf("no Revision label found for Metric %s", metric.Name)
	}

	return ServiceScraper{
		httpClient: httpClient,
		url:        fmt.Sprintf("http://%s-service.%s:9090/metrics", revName, metric.Namespace),
		metric:     metric,
		metricKey:  NewMetricKey(metric.Namespace, metric.Name),
		logger:     logger,
	}, nil
}

// Scrape call the destination service then send it
// to the given stats chanel
func (s *ServiceScraper) Scrape(statsCh chan<- *StatMessage) {
	stat, err := s.scrapeViaURL()
	if err != nil {
		s.logger.Errorw("Failed to get metrics", zap.Error(err))
		return
	}

	s.sendStatMessage(stat, statsCh)
}

func (s *ServiceScraper) scrapeViaURL() (Stat, error) {
	req, err := http.NewRequest("GET", s.url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Stat{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Stat{}, fmt.Errorf("GET request for URL %q returned HTTP status %v", s.url, resp.StatusCode)
	}

	return extractData(resp)
}

func extractData(resp *http.Response) (Stat, error) {
	stat := Stat{}
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return stat, fmt.Errorf("Reading text format failed: %v", err)
	}

	now := time.Now()
	stat.Time = &now

	if metric, ok := metricFamilies["queue_average_concurrent_requests"]; ok {
		for _, label := range metric.Metric[0].Label {
			if *label.Name == "destination_pod" {
				stat.PodName = *label.Value
				break
			}
		}
		stat.AverageConcurrentRequests = *metric.Metric[0].Gauge.Value
	} else {
		return stat, errors.New("queue_average_concurrent_requests key not found in response")
	}

	if metric, ok := metricFamilies["queue_operations_per_second"]; ok {
		stat.RequestCount = int32(*metric.Metric[0].Gauge.Value)
	} else {
		return stat, errors.New("queue_operations_per_second key not found in response")
	}

	return stat, nil
}

func (s *ServiceScraper) sendStatMessage(stat Stat, statsCh chan<- *StatMessage) {
	sm := &StatMessage{
		Stat: stat,
		Key:  s.metricKey,
	}
	statsCh <- sm
}
