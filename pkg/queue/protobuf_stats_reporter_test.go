/*
Copyright 2020 The Knative Authors

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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/network"
)

var testStat = metrics.Stat{
	PodName:                          "testPod",
	AverageConcurrentRequests:        5.0,
	AverageProxiedConcurrentRequests: 5.0,
	ProxiedRequestCount:              100.0,
	RequestCount:                     100.0,
	ProcessUptime:                    20.0,
}

func TestReporterReport(t *testing.T) {
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			reporter := NewProtobufStatsReporter(pod, test.reportingPeriod)
			// Make the value slightly more interesting, rather than microseconds.
			reporter.startTime = reporter.startTime.Add(-5 * time.Second)
			reporter.Report(network.RequestStatsReport{
				AverageConcurrency:        test.concurrency,
				AverageProxiedConcurrency: test.proxiedConcurrency,
				RequestCount:              test.reqCount,
				ProxiedRequestCount:       test.proxiedReqCount,
			})
			stat := reporter.stat.Load().(metrics.Stat)
			if stat.RequestCount != test.expectedReqCount {
				t.Errorf("stat.RequestCount = %v, want %v", stat.RequestCount, test.expectedReqCount)
			}
			if stat.AverageConcurrentRequests != test.expectedConcurrency {
				t.Errorf("stat.AverageConcurrentRequests = %v, want %v", stat.AverageConcurrentRequests, test.expectedConcurrency)
			}
			if stat.ProxiedRequestCount != test.expectedProxiedRequestCount {
				t.Errorf("stat.ProxiedRequestCount = %v, want %v", stat.ProxiedRequestCount, test.expectedProxiedRequestCount)
			}
			if stat.AverageProxiedConcurrentRequests != test.expectedProxiedConcurrency {
				t.Errorf("stat.AverageProxiedConcurrentRequests = %v, want %v", stat.AverageProxiedConcurrentRequests, test.expectedProxiedConcurrency)
			}
			if got := stat.ProcessUptime; got < 5.0 || got > 6.0 {
				t.Errorf("Got %v for process uptime, wanted 5.0 <= x < 6.0", got)
			}
		})
	}
}

func TestProtoHandler(t *testing.T) {
	metricsStat := atomic.Value{}
	metricsStat.Store(testStat)

	tests := []struct {
		name     string
		reporter ProtobufStatsReporter
		errorMsg string
	}{{
		name:     "No metrics available",
		reporter: ProtobufStatsReporter{},
		errorMsg: "An error has occurred while serving metrics:\n\nno metrics available yet",
	}, {
		name: "Metrics available",
		reporter: ProtobufStatsReporter{
			reportingPeriod: time.Duration(1),
			startTime:       time.Now(),
			stat:            metricsStat,
			podName:         "testPod"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/metrics", nil)
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(test.reporter.Handler().(http.HandlerFunc))
			handler.ServeHTTP(rr, req)
			if test.errorMsg != "" { // error case
				expected := test.errorMsg + "\n"
				if status := rr.Code; status != http.StatusInternalServerError {
					t.Errorf("StatusCode = %d want %d",
						status, http.StatusInternalServerError)
				}
				if rr.Body.String() != expected {
					t.Errorf("Body = %q want %q",
						rr.Body.String(), expected)
				}
			} else { // good case, data received
				bodyBytes, err := ioutil.ReadAll(rr.Body)
				if err != nil {
					t.Errorf("Reading body failed: %v", err)
				}
				stat := metrics.Stat{}
				err = stat.Unmarshal(bodyBytes)
				if err != nil {
					t.Errorf("Unmarshalling failed: %v", err)
				}
				if diff := cmp.Diff(stat, testStat); diff != "" {
					t.Errorf("Handler returned wrong stat data: (-want, +got):\n%v", diff)
				}
			}
		})
	}
}
