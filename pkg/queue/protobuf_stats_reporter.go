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
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"

	"knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/network"
)

const (
	contentTypeHeader = "Content-Type"
)

// PrometheusStatsReporter structure represents a prometheus stats reporter.
type ProtobufStatsReporter struct {
	reportingPeriod time.Duration
	startTime       time.Time
	stat            atomic.Value
	podName         string
}

// NewPrometheusStatsReporter creates a reporter that collects and reports queue metrics.
func NewProtobufStatsReporter(pod string, reportingPeriod time.Duration) *ProtobufStatsReporter {
	return &ProtobufStatsReporter{
		reportingPeriod: reportingPeriod,
		startTime:       time.Now(),
		stat:            atomic.Value{},
		podName:         pod,
	}
}

// Report captures request metrics.
func (r *ProtobufStatsReporter) Report(stats network.RequestStatsReport) {
	// Requests per second is a rate over time while concurrency is not.
	rp := r.reportingPeriod.Seconds()
	r.stat.Store(metrics.Stat{
		PodName:                          r.podName,
		RequestCount:                     stats.RequestCount / rp,
		ProxiedRequestCount:              stats.ProxiedRequestCount / rp,
		AverageConcurrentRequests:        stats.AverageConcurrency,
		AverageProxiedConcurrentRequests: stats.AverageProxiedConcurrency,
		ProcessUptime:                    time.Since(r.startTime).Seconds(),
	})
}

// Handler returns an uninstrumented http.Handler used to serve stats registered by this
// ProtobufStatsReporter.
func (r *ProtobufStatsReporter) Handler() http.Handler {
	return http.HandlerFunc(func(rsp http.ResponseWriter, req *http.Request) {
		stat := r.stat.Load()
		if stat == nil {
			httpError(rsp, "no metrics available yet")
			return
		}
		header := rsp.Header()
		data := stat.(metrics.Stat)
		buffer, err := proto.Marshal(&data)
		if err != nil {
			httpError(rsp, err.Error())
			return
		}
		header.Set(contentTypeHeader, network.ProtoAcceptContent)
		rsp.Write(buffer)
	})
}

func httpError(rsp http.ResponseWriter, errMsg string) {
	http.Error(
		rsp,
		"An error has occurred while serving metrics:\n\n"+errMsg,
		http.StatusInternalServerError,
	)
}
