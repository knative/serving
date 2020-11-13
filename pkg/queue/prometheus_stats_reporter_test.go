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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/autoscaler/metrics"

	dto "github.com/prometheus/client_model/go"
)

const (
	namespace = "default"
	config    = "helloworld-go"
	revision  = "helloworld-go-00001"
	pod       = "helloworld-go-00001-deployment-8ff587cc9-7g9gc"
)

var ignoreStatFields = cmpopts.IgnoreFields(metrics.Stat{}, "ProcessUptime")

var testCases = []struct {
	name            string
	reportingPeriod time.Duration
	report          network.RequestStatsReport
	want            metrics.Stat
}{{
	name:            "no proxy requests",
	reportingPeriod: 1 * time.Second,
	report: network.RequestStatsReport{
		AverageConcurrency: 3,
		RequestCount:       39,
	},
	want: metrics.Stat{
		AverageConcurrentRequests: 3,
		RequestCount:              39,
	},
}, {
	name:            "reportingPeriod=10s",
	reportingPeriod: 10 * time.Second,
	report: network.RequestStatsReport{
		AverageConcurrency:        3,
		AverageProxiedConcurrency: 2,
		ProxiedRequestCount:       15,
		RequestCount:              39,
	},
	want: metrics.Stat{
		AverageConcurrentRequests:        3,
		AverageProxiedConcurrentRequests: 2,
		ProxiedRequestCount:              1.5,
		RequestCount:                     3.9,
	},
}, {
	name:            "reportingPeriod=2s",
	reportingPeriod: 2 * time.Second,

	report: network.RequestStatsReport{
		AverageConcurrency:        3,
		AverageProxiedConcurrency: 2,
		ProxiedRequestCount:       15,
		RequestCount:              39,
	},
	want: metrics.Stat{
		AverageConcurrentRequests:        3,
		AverageProxiedConcurrentRequests: 2,
		ProxiedRequestCount:              7.5,
		RequestCount:                     19.5,
	},
}, {
	name:            "reportingPeriod=1s",
	reportingPeriod: 1 * time.Second,

	report: network.RequestStatsReport{
		AverageConcurrency:        3,
		AverageProxiedConcurrency: 2,
		ProxiedRequestCount:       15,
		RequestCount:              39,
	},
	want: metrics.Stat{
		AverageConcurrentRequests:        3,
		AverageProxiedConcurrentRequests: 2,
		ProxiedRequestCount:              15,
		RequestCount:                     39,
	},
}}

func TestNewPrometheusStatsReporterNegative(t *testing.T) {
	tests := []struct {
		name      string
		errorMsg  string
		result    error
		namespace string
		config    string
		revision  string
		pod       string
	}{{
		name:     "Empty_Namespace_Value",
		errorMsg: "Expected namespace empty error",
		result:   errors.New("namespace must not be empty"),
		config:   config,
		revision: revision,
		pod:      pod,
	}, {
		name:      "Empty_Config_Value",
		errorMsg:  "Expected config empty error",
		result:    errors.New("config must not be empty"),
		namespace: namespace,
		revision:  revision,
		pod:       pod,
	}, {
		name:      "Empty_Revision_Value",
		errorMsg:  "Expected revision empty error",
		result:    errors.New("revision must not be empty"),
		namespace: namespace,
		config:    config,
		pod:       pod,
	}, {
		name:      "Empty_Pod_Value",
		errorMsg:  "Expected pod empty error",
		result:    errors.New("pod must not be empty"),
		namespace: namespace,
		config:    config,
		revision:  revision,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPrometheusStatsReporter(test.namespace, test.config, test.revision, test.pod, 1*time.Second); err.Error() != test.result.Error() {
				t.Errorf("Got error msg from NewPrometheusStatsReporter(): '%+v', wanted '%+v'", err, test.errorMsg)
			}
		})
	}
}

func TestPrometheusStatsReporterReport(t *testing.T) {
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			reporter, err := NewPrometheusStatsReporter(namespace, config, revision, pod, test.reportingPeriod)
			if err != nil {
				t.Errorf("Something went wrong with creating a reporter, '%v'.", err)
			}
			// Make the value slightly more interesting, rather than microseconds.
			reporter.startTime = reporter.startTime.Add(-5 * time.Second)
			reporter.Report(test.report)
			got := metrics.Stat{
				RequestCount:                     getData(t, requestsPerSecondGV),
				AverageConcurrentRequests:        getData(t, averageConcurrentRequestsGV),
				ProxiedRequestCount:              getData(t, proxiedRequestsPerSecondGV),
				AverageProxiedConcurrentRequests: getData(t, averageProxiedConcurrentRequestsGV),
				ProcessUptime:                    getData(t, processUptimeGV),
			}
			if !cmp.Equal(test.want, got, ignoreStatFields) {
				t.Errorf("Scraped stat mismatch; diff(-want,+got):\n%s", cmp.Diff(test.want, got))
			}
			if gotUptime := got.ProcessUptime; gotUptime < 5.0 || gotUptime > 6.0 {
				t.Errorf("Got %v for process uptime, wanted 5.0 <= x < 6.0", gotUptime)
			}
		})
	}
}

func getData(t *testing.T, gv *prometheus.GaugeVec) float64 {
	t.Helper()
	g, err := gv.GetMetricWith(prometheus.Labels{
		destinationNsLabel:     namespace,
		destinationConfigLabel: config,
		destinationRevLabel:    revision,
		destinationPodLabel:    pod,
	})
	if err != nil {
		t.Fatal("GaugeVec.GetMetricWith() error =", err)
	}
	m := dto.Metric{}
	if err := g.Write(&m); err != nil {
		t.Fatal("Gauge.Write() error =", err)
	}
	return m.Gauge.GetValue()
}
