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

package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/knative/serving/pkg/activator"
	pkghttp "github.com/knative/serving/pkg/http"
)

// ReportingHTTPHandler will forward request & response metrics
// to the `activator.StatsReporter``
type ReportingHTTPHandler struct {
	NextHandler http.Handler
	Reporter    activator.StatsReporter
}

func (h *ReportingHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, activator.RevisionHeaderName)
	config := pkghttp.LastHeaderValue(r.Header, activator.ConfigurationHeader)

	start := time.Now()

	capture := &statusCapture{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	h.NextHandler.ServeHTTP(capture, r)

	status := capture.statusCode
	duration := time.Now().Sub(start)

	if numRetries := w.Header().Get(activator.ResponseCountHTTPHeader); numRetries != "" {
		count, _ := strconv.Atoi(numRetries)
		h.Reporter.ReportResponseCount(namespace, config, name, int(status), count, 1.0)
	}

	h.Reporter.ReportResponseTime(namespace, config, name, int(status), duration)
}

type statusCapture struct {
	http.ResponseWriter
	statusCode int
}

func (s *statusCapture) WriteHeader(statusCode int) {
	s.statusCode = statusCode
	s.ResponseWriter.WriteHeader(statusCode)

}
