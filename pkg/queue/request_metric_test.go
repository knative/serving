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
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue/stats"
)

func TestNewRequestMetricHandlerFailure(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var r stats.StatsReporter
	_, err := NewRequestMetricHandler(baseHandler, r, nil)
	if err == nil {
		t.Error("should get error when StatsReporter is empty")
	}
}

func TestRequestMetricHandler(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	for _, b := range []*Breaker{nil, NewBreaker(BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1})} {
		r := &fakeStatsReporter{}
		// No breaker is fine.
		handler, err := NewRequestMetricHandler(baseHandler, r, b)
		if err != nil {
			t.Fatalf("failed to create handler: %v", err)
		}

		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString("test"))
		handler.ServeHTTP(resp, req)

		// Serve one request, should get 1 request count and none zero latency
		if got, want := r.reqCountReportTimes, 1; got != want {
			t.Errorf("ReportRequestCount was triggered %v times, want %v", got, want)
		}
		if got, want := r.respTimeReportTimes, 1; got != want {
			t.Errorf("ReportResponseTime was triggered %v times, want %v", got, want)
		}
		if got, want := r.lastRespCode, http.StatusOK; got != want {
			t.Errorf("response code got %v, want %v", got, want)
		}
		if got, want := r.lastReqCount, 1; got != int64(want) {
			t.Errorf("request count got %v, want %v", got, want)
		}
		if r.lastReqLatency == 0 {
			t.Errorf("request latency got %v, want larger than 0", r.lastReqLatency)
		}
		wantQD := 0
		if b != nil {
			wantQD++
		}
		if got, want := r.queueDepthTimes, wantQD; got != want {
			t.Errorf("QueueDepth report count = %d, want: %d", got, want)
		}

		// A probe request should not be recorded.
		req.Header.Set(network.ProbeHeaderName, "activator")
		handler.ServeHTTP(resp, req)
		if got, want := r.reqCountReportTimes, 1; got != want {
			t.Errorf("ReportRequestCount was triggered %v times, want %v", got, want)
		}
		if got, want := r.respTimeReportTimes, 1; got != want {
			t.Errorf("ReportResponseTime was triggered %v times, want %v", got, want)
		}
	}
}

func TestRequestMetricHandlerPanickingHandler(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	r := &fakeStatsReporter{}
	handler, err := NewRequestMetricHandler(baseHandler, r, nil)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString("test"))
	defer func() {
		err := recover()
		if err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}

		// Serve one request, should get 1 request count and none zero latency
		if got, want := r.lastRespCode, http.StatusInternalServerError; got != want {
			t.Errorf("Response code got %v, want %v", got, want)
		}
		if got, want := r.lastReqCount, int64(1); got != want {
			t.Errorf("Request count got %d, want %d", got, want)
		}
		if r.lastReqLatency == 0 {
			t.Errorf("Request latency got %v, want larger than 0", r.lastReqLatency)
		}
	}()
	handler.ServeHTTP(resp, req)
}

// fakeStatsReporter just record the last stat it received and the times it
// calls ReportRequestCount and ReportResponseTime
type fakeStatsReporter struct {
	reqCountReportTimes int
	respTimeReportTimes int
	queueDepthTimes     int
	lastRespCode        int
	lastReqCount        int64
	lastReqLatency      time.Duration
}

func (r *fakeStatsReporter) ReportQueueDepth(qd int) error {
	r.queueDepthTimes++
	return nil
}

func (r *fakeStatsReporter) ReportRequestCount(responseCode int) error {
	r.reqCountReportTimes++
	r.lastRespCode = responseCode
	r.lastReqCount = 1
	return nil
}

func (r *fakeStatsReporter) ReportResponseTime(responseCode int, d time.Duration) error {
	r.respTimeReportTimes++
	r.lastRespCode = responseCode
	r.lastReqLatency = d
	return nil
}
