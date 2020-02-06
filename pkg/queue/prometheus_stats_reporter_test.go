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

package queue

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

const (
	namespace = "default"
	config    = "helloworld-go"
	revision  = "helloworld-go-00001"
	pod       = "helloworld-go-00001-deployment-8ff587cc9-7g9gc"

	// Except for uptime everything else is integers, so this precision is
	// good enough for the tests.
	precision = 0.1
)

func TestNewPrometheusStatsReporter_negative(t *testing.T) {
	tests := []struct {
		name      string
		errorMsg  string
		result    error
		namespace string
		config    string
		revision  string
		pod       string
	}{
		{
			"Empty_Namespace_Value",
			"Expected namespace empty error",
			errors.New("namespace must not be empty"),
			"",
			config,
			revision,
			pod,
		},
		{
			"Empty_Config_Value",
			"Expected config empty error",
			errors.New("config must not be empty"),
			namespace,
			"",
			revision,
			pod,
		},
		{
			"Empty_Revision_Value",
			"Expected revision empty error",
			errors.New("revision must not be empty"),
			namespace,
			config,
			"",
			pod,
		},
		{
			"Empty_Pod_Value",
			"Expected pod empty error",
			errors.New("pod must not be empty"),
			namespace,
			config,
			revision,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPrometheusStatsReporter(test.namespace, test.config, test.revision, test.pod, 1*time.Second); err.Error() != test.result.Error() {
				t.Errorf("Got error msg from NewPrometheusStatsReporter(): '%+v', wanted '%+v'", err, test.errorMsg)
			}
		})
	}
}

func TestReporterReport(t *testing.T) {
	tests := []struct {
		name            string
		reportingPeriod time.Duration

		concurrency        float64
		proxiedConcurrency float64
		reqCount           float64
		proxiedReqCount    float64

		expectedReqCount            float64
		expectedProxiedRequestCount float64
		expectedConcurrency         float64
		expectedProxiedConcurrency  float64
	}{{
		name:            "no proxy requests",
		reportingPeriod: 1 * time.Second,

		reqCount:    39,
		concurrency: 3,

		expectedReqCount:            39,
		expectedConcurrency:         3,
		expectedProxiedRequestCount: 0,
		expectedProxiedConcurrency:  0,
	}, {
		name:            "reportingPeriod=10s",
		reportingPeriod: 10 * time.Second,

		reqCount:           39,
		concurrency:        3,
		proxiedReqCount:    15,
		proxiedConcurrency: 2,

		expectedReqCount:            3.9,
		expectedConcurrency:         3,
		expectedProxiedRequestCount: 1.5,
		expectedProxiedConcurrency:  2,
	}, {
		name:            "reportingPeriod=2s",
		reportingPeriod: 2 * time.Second,

		reqCount:           39,
		concurrency:        3,
		proxiedReqCount:    15,
		proxiedConcurrency: 2,

		expectedReqCount:            19.5,
		expectedConcurrency:         3,
		expectedProxiedRequestCount: 7.5,
		expectedProxiedConcurrency:  2,
	}, {
		name:            "reportingPeriod=1s",
		reportingPeriod: 1 * time.Second,

		reqCount:           39,
		concurrency:        3,
		proxiedReqCount:    15,
		proxiedConcurrency: 2,

		expectedReqCount:            39,
		expectedConcurrency:         3,
		expectedProxiedRequestCount: 15,
		expectedProxiedConcurrency:  2,
	},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter, err := NewPrometheusStatsReporter(namespace, config, revision, pod, test.reportingPeriod)
			if err != nil {
				t.Errorf("Something went wrong with creating a reporter, '%v'.", err)
			}
			// Make the value slightly more interesting, rather than microseconds.
			reporter.startTime = reporter.startTime.Add(-5 * time.Second)
			reporter.Report(test.concurrency, test.proxiedConcurrency, test.reqCount, test.proxiedReqCount)
			checkData(t, requestsPerSecondGV, test.expectedReqCount)
			checkData(t, averageConcurrentRequestsGV, test.expectedConcurrency)
			checkData(t, proxiedRequestsPerSecondGV, test.expectedProxiedRequestCount)
			checkData(t, averageProxiedConcurrentRequestsGV, test.expectedProxiedConcurrency)
			checkData(t, processUptimeGV, 5.0)
		})
	}
}

func checkData(t *testing.T, gv *prometheus.GaugeVec, want float64) {
	t.Helper()
	g, err := gv.GetMetricWith(prometheus.Labels{
		destinationNsLabel:     namespace,
		destinationConfigLabel: config,
		destinationRevLabel:    revision,
		destinationPodLabel:    pod,
	})
	if err != nil {
		t.Fatalf("GaugeVec.GetMetricWith() error = %v", err)
	}

	m := dto.Metric{}
	if err := g.Write(&m); err != nil {
		t.Fatalf("Gauge.Write() error = %v", err)
	}
	if got := *m.Gauge.Value; math.Abs(got-want) > precision {
		t.Errorf("Got %v for Gauge value, wanted %v", got, want)
	}
}
