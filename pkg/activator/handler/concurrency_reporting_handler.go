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
	"time"

	"github.com/knative/serving/pkg/activator"

	"github.com/knative/serving/pkg/autoscaler"
)

const (
	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100
)

func NewConcurrencyReportingHandler(statChan chan *autoscaler.StatMessage, reportChan chan time.Time, next http.Handler) *concurrencyReportingHandler {

	channels := Channels{
		ReqChan:    make(chan ReqEvent, requestCountingQueueLength),
		StatChan:   statChan,
		ReportChan: reportChan,
	}

	NewConcurrencyReporter(autoscaler.ActivatorPodName, channels)

	handler := &concurrencyReportingHandler{
		NextHandler: next,
		ReqChan:     channels.ReqChan,
	}

	return handler
}

type concurrencyReportingHandler struct {
	NextHandler http.Handler
	ReqChan     chan ReqEvent
}

func (h *concurrencyReportingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := r.Header.Get(activator.RevisionHeaderNamespace)
	name := r.Header.Get(activator.RevisionHeaderName)

	revisionKey := autoscaler.NewKpaKey(namespace, name)

	h.ReqChan <- ReqEvent{Key: revisionKey, EventType: ReqIn}
	defer func() { h.ReqChan <- ReqEvent{Key: revisionKey, EventType: ReqOut} }()
	h.NextHandler.ServeHTTP(w, r)
}
