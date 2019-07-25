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
	"strings"
	"time"

	"go.uber.org/zap"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
)

// ConcurrencyReporter reports stats based on incoming requests and ticks.
type ConcurrencyReporter struct {
	logger  *zap.SugaredLogger
	podName string

	// Ticks with every request arrived/completed respectively
	reqChan chan ReqEvent
	// Ticks with every stat report request
	reportChan <-chan time.Time
	// Stat reporting channel
	statChan chan *autoscaler.StatMessage

	rl servinglisters.RevisionLister
	sr activator.StatsReporter

	clock system.Clock
}

// NewConcurrencyReporter creates a ConcurrencyReporter which listens to incoming
// ReqEvents on reqChan and ticks on reportChan and reports stats on statChan.
func NewConcurrencyReporter(logger *zap.SugaredLogger, podName string, reqChan chan ReqEvent, reportChan <-chan time.Time,
	statChan chan *autoscaler.StatMessage, rl servinglisters.RevisionLister, sr activator.StatsReporter) *ConcurrencyReporter {
	return NewConcurrencyReporterWithClock(logger, podName, reqChan, reportChan, statChan, rl, sr, system.RealClock{})
}

// NewConcurrencyReporterWithClock instantiates a new concurrency reporter
// which uses the passed clock.
func NewConcurrencyReporterWithClock(logger *zap.SugaredLogger, podName string, reqChan chan ReqEvent, reportChan <-chan time.Time,
	statChan chan *autoscaler.StatMessage, rl servinglisters.RevisionLister, sr activator.StatsReporter, clock system.Clock) *ConcurrencyReporter {
	return &ConcurrencyReporter{
		logger:     logger,
		podName:    podName,
		reqChan:    reqChan,
		reportChan: reportChan,
		statChan:   statChan,
		rl:         rl,
		sr:         sr,
		clock:      clock,
	}
}

func (cr *ConcurrencyReporter) reportToAutoscaler(key string, concurrency, requestCount int32) {
	stat := autoscaler.Stat{
		PodName:                   cr.podName,
		AverageConcurrentRequests: float64(concurrency),
		RequestCount:              float64(requestCount),
	}

	// Send the stat to another goroutine to transmit
	// so we can continue bucketing stats.
	cr.statChan <- &autoscaler.StatMessage{
		Key:  key,
		Stat: stat,
	}
}

func (cr *ConcurrencyReporter) reportToMetricsBackend(key string, concurrency int32) {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		cr.logger.Errorf("Error while extracting namespace and revision from key: %v", key)
		return
	}
	ns := parts[0]
	revName := parts[1]
	revision, err := cr.rl.Revisions(ns).Get(revName)
	if err != nil {
		cr.logger.Errorw("Error while getting revision", zap.Error(err))
		return
	}
	configurationName := revision.Labels[serving.ConfigurationLabelKey]
	serviceName := revision.Labels[serving.ServiceLabelKey]
	cr.sr.ReportRequestConcurrency(ns, serviceName, configurationName, revName, int64(concurrency))
}

// Run runs until stopCh is closed and processes events on all incoming channels
func (cr *ConcurrencyReporter) Run(stopCh <-chan struct{}) {
	// Contains the number of in-flight requests per-key
	outstandingRequestsPerKey := make(map[string]int32)
	// Contains the number of incoming requests in the current
	// reporting period, per key.
	incomingRequestsPerKey := make(map[string]int32)

	for {
		select {
		case event := <-cr.reqChan:
			switch event.EventType {
			case ReqIn:
				incomingRequestsPerKey[event.Key]++

				// Report the first request for a key immediately.
				if _, ok := outstandingRequestsPerKey[event.Key]; !ok {
					cr.reportToAutoscaler(event.Key, 1, incomingRequestsPerKey[event.Key])
				}
				outstandingRequestsPerKey[event.Key]++
			case ReqOut:
				outstandingRequestsPerKey[event.Key]--
			}
		case <-cr.reportChan:
			for key, concurrency := range outstandingRequestsPerKey {
				if concurrency == 0 {
					delete(outstandingRequestsPerKey, key)
				} else {
					cr.reportToAutoscaler(key, concurrency, incomingRequestsPerKey[key])
				}
				cr.reportToMetricsBackend(key, concurrency)
			}

			incomingRequestsPerKey = make(map[string]int32)
		case <-stopCh:
			return
		}
	}
}
