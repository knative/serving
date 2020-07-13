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
	"fmt"
	"io"
	"net/http"
	"sync"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"knative.dev/serving/pkg/network"
)

type httpScrapeClient struct {
	httpClient *http.Client
}

var step = 64

var dataSizeClasses = []int{
	step,
	step * 2,
	step * 3,
	step * 4,
	304, // 292 is the maximum proto buf stat record, use multiple of 16
}

var pools = [...]sync.Pool{
	{
		New: func() interface{} {
			return make([]byte, dataSizeClasses[0], dataSizeClasses[0])
		},
	},
	{
		New: func() interface{} {
			return make([]byte, dataSizeClasses[1], dataSizeClasses[1])
		},
	},
	{
		New: func() interface{} {
			return make([]byte, dataSizeClasses[2], dataSizeClasses[2])
		},
	},
	{
		New: func() interface{} {
			return make([]byte, dataSizeClasses[3], dataSizeClasses[3])
		},
	},
	{
		New: func() interface{} {
			return make([]byte, dataSizeClasses[4], dataSizeClasses[4])
		},
	},
}

func getDataBuffer(size int64) ([]byte, int) {
	i := 0
	for ; i < len(dataSizeClasses)-1; i++ {
		if size <= int64(dataSizeClasses[i]) {
			break
		}
	}
	return pools[i].Get().([]byte), i
}

func newHTTPScrapeClient(httpClient *http.Client) (*httpScrapeClient, error) {
	if httpClient == nil {
		return nil, errors.New("HTTP client must not be nil")
	}

	return &httpScrapeClient{
		httpClient: httpClient,
	}, nil
}

func (c *httpScrapeClient) Scrape(url string) (Stat, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return emptyStat, err
	}
	// Ask for protobuf by default. Note that during migration this will not trigger the proto format supported
	// by Prometheus reporter as the latter uses `application/vnd.google.protobuf`.
	req.Header.Add("Accept", network.ProtoAcceptContent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return emptyStat, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return emptyStat, fmt.Errorf("GET request for URL %q returned HTTP status %v", url, resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") == network.ProtoAcceptContent {
		return statFromProto(resp.Body, resp.ContentLength)
	}

	return statFromPrometheus(resp.Body)
}

func statFromProto(body io.Reader, l int64) (Stat, error) {
	var stat Stat
	if l <= 0 {
		return emptyStat, fmt.Errorf("no data received, data size unknown")
	}
	b, i := getDataBuffer(l)
	defer func() {
		pools[i].Put(b)
	}()
	var err error
	var n int = 0
	for n < int(l) && err == nil {
		var nn int
		nn, err = body.Read(b[n:])
		n += nn
	}
	if err != nil {
		return emptyStat, fmt.Errorf("reading body failed: %w", err)
	}
	err = stat.Unmarshal(b[0:l])
	if err != nil {
		return emptyStat, fmt.Errorf("unmarshalling failed: %w", err)
	}
	return stat, nil
}

func statFromPrometheus(body io.Reader) (Stat, error) {
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(body)
	if err != nil {
		return emptyStat, fmt.Errorf("reading text format failed: %w", err)
	}

	stat := emptyStat
	for m, pv := range map[string]*float64{
		"queue_average_concurrent_requests":         &stat.AverageConcurrentRequests,
		"queue_average_proxied_concurrent_requests": &stat.AverageProxiedConcurrentRequests,
		"queue_requests_per_second":                 &stat.RequestCount,
		"queue_proxied_operations_per_second":       &stat.ProxiedRequestCount,
	} {
		pm := prometheusMetric(metricFamilies, m)
		if pm == nil {
			return emptyStat, fmt.Errorf("could not find value for %s in response", m)
		}
		*pv = *pm.Gauge.Value

		if stat.PodName == "" {
			stat.PodName = prometheusLabel(pm.Label, "destination_pod")
			if stat.PodName == "" {
				return emptyStat, errors.New("could not find pod name in metric labels")
			}
		}
	}
	// Transitional metrics, which older pods won't report.
	for m, pv := range map[string]*float64{
		"process_uptime": &stat.ProcessUptime, // Can be removed after 0.15 cuts.
	} {
		pm := prometheusMetric(metricFamilies, m)
		// Ignore if not found.
		if pm == nil {
			continue
		}
		*pv = *pm.Gauge.Value
	}
	return stat, nil
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

// prometheusLabels returns the value of the label with the given key from the
// given slice of labels. Returns an empty string if the label cannot be found.
func prometheusLabel(labels []*dto.LabelPair, key string) string {
	for _, label := range labels {
		if *label.Name == key {
			return *label.Value
		}
	}

	return ""
}
