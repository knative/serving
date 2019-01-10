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
	"fmt"
	"net/http"
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/prometheus/common/expfmt"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	trueInFloat64 = float64(1)
	// TODO(yanweiguo): tuning the sample size
	sampleSize          = 3
	scraperTickInterval = time.Second
)

// StatsScraper scrapes Revision metrics via a K8S service by sampling.
type StatsScraper struct {
	url             string
	metric          *Metric
	metricKey       string
	selector        labels.Selector
	serviceName     string
	endpointsLister corev1listers.EndpointsLister
	logger          *zap.SugaredLogger
}

// CreateNewStatsScraper creates a new StatsScraper for the Revision which
// the given PodAutoscaler is responsible for.
func CreateNewStatsScraper(
	metric *Metric, endpointsInformer corev1informers.EndpointsInformer, logger *zap.SugaredLogger) (StatsScraper, error) {
	revName := metric.Labels[serving.RevisionLabelKey]
	if revName == "" {
		logger.Errorf("no Revision label found for Metric %s", metric.Name)
		return StatsScraper{}, nil
	}

	selector := labels.SelectorFromSet(labels.Set{serving.RevisionLabelKey: revName})
	return StatsScraper{
		url:             fmt.Sprintf("http://%s-service.%s:9090/metrics", revName, metric.Namespace),
		metric:          metric,
		metricKey:       NewMetricKey(metric.Namespace, metric.Name),
		selector:        selector,
		serviceName:     fmt.Sprintf("%s-service", revName),
		endpointsLister: endpointsInformer.Lister(),
		logger:          logger,
	}, nil
}

// Scrape collects sample metrics data from the destination service then send it
// to the given stats chanel
func (s *StatsScraper) Scrape(statsCh chan<- *StatMessage) {
	revName := s.metric.Labels[serving.RevisionLabelKey]
	endpoints, err := s.endpointsLister.Endpoints(s.metric.Namespace).Get(s.serviceName)

	podsNum := 0
	if errors.IsNotFound(err) {
		// Treat not found as zero endpoints, it either hasn't been created
		// or it has been torn down.
		return
	} else if err != nil {
		s.logger.Errorf("Error checking Endpoints %q: %v", s.serviceName, err)
		return
	} else {
		for _, es := range endpoints.Subsets {
			podsNum += len(es.Addresses)
		}
	}

	s.logger.Infof("found %d pods for Revision %s", podsNum, revName)
	if podsNum == 0 {
		// No endpoints to serving scraping requests
		return
	}

	for i := 0; i < sampleSize; i++ {
		stat, err := s.scrapeViaURL()
		if err != nil {
			s.logger.Errorf("failed to get metrics: %v", err)
			continue
		}

		stat.ObservedPods = int32(podsNum)
		sm := &StatMessage{
			Stat: stat,
			Key:  s.metricKey,
		}
		s.logger.Infof("StatMessage %v", sm)
		// statsCh <- sm
	}
}

func (s *StatsScraper) scrapeViaURL() (Stat, error) {
	// Create a new Transport each time otherwise the request will be routed to same pod.
	tr := &http.Transport{TLSHandshakeTimeout: time.Second}
	client := &http.Client{Transport: tr, Timeout: time.Second}
	resp, err := client.Get(s.url)

	if err != nil {
		return Stat{}, fmt.Errorf("failed to send GET request for URL %q: %v", s.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Stat{}, fmt.Errorf("GET request for URL %q returned HTTP status %s", s.url, resp.Status)
	}

	return extractData(resp)
}

func extractData(resp *http.Response) (Stat, error) {
	stat := Stat{}
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return stat, fmt.Errorf("reading text format failed: %v", err)
	}

	now := time.Now()
	concurrent := metricFamilies["queue_average_concurrent_requests"].Metric[0]
	requestCount := metricFamilies["queue_operations_per_second"].Metric[0]
	lameDuck := metricFamilies["queue_lame_duck"].Metric[0]
	for _, label := range concurrent.Label {
		if *label.Name == "destination_pod" {
			stat.PodName = *label.Value
			break
		}
	}
	stat.Time = &now
	stat.AverageConcurrentRequests = *concurrent.Gauge.Value
	stat.RequestCount = int32(*requestCount.Gauge.Value)
	stat.LameDuck = trueInFloat64 == *lameDuck.Gauge.Value
	return stat, nil
}
