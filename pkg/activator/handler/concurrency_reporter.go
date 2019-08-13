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
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler"
	servinglisters "knative.dev/serving/pkg/client/serving/listers/serving/internalversion"
)

// ConcurrencyReporter reports stats based on incoming requests and ticks.
type ConcurrencyReporter struct {
	logger  *zap.SugaredLogger
	podName string

	// Ticks with every request arrived/completed respectively
	reqCh chan ReqEvent
	// Ticks with every stat report request
	reportCh <-chan time.Time
	// Stat reporting channel
	statCh chan *autoscaler.StatMessage

	rl servinglisters.RevisionLister
	sr activator.StatsReporter

	clock system.Clock
}

// NewConcurrencyReporter creates a ConcurrencyReporter which listens to incoming
// ReqEvents on reqCh and ticks on reportCh and reports stats on statCh.
func NewConcurrencyReporter(logger *zap.SugaredLogger, podName string, reqCh chan ReqEvent, reportCh <-chan time.Time,
	statCh chan *autoscaler.StatMessage, rl servinglisters.RevisionLister, sr activator.StatsReporter) *ConcurrencyReporter {
	return NewConcurrencyReporterWithClock(logger, podName, reqCh, reportCh, statCh, rl, sr, system.RealClock{})
}

// NewConcurrencyReporterWithClock instantiates a new concurrency reporter
// which uses the passed clock.
func NewConcurrencyReporterWithClock(logger *zap.SugaredLogger, podName string, reqCh chan ReqEvent, reportCh <-chan time.Time,
	statCh chan *autoscaler.StatMessage, rl servinglisters.RevisionLister, sr activator.StatsReporter, clock system.Clock) *ConcurrencyReporter {
	return &ConcurrencyReporter{
		logger:   logger,
		podName:  podName,
		reqCh:    reqCh,
		reportCh: reportCh,
		statCh:   statCh,
		rl:       rl,
		sr:       sr,
		clock:    clock,
	}
}

func (cr *ConcurrencyReporter) reportToAutoscaler(key types.NamespacedName, concurrency, requestCount int32) {
	stat := autoscaler.Stat{
		PodName:                   cr.podName,
		AverageConcurrentRequests: float64(concurrency),
		RequestCount:              float64(requestCount),
	}

	// Send the stat to another goroutine to transmit
	// so we can continue bucketing stats.
	cr.statCh <- &autoscaler.StatMessage{
		Key:  key,
		Stat: stat,
	}
}

func (cr *ConcurrencyReporter) reportToMetricsBackend(key types.NamespacedName, concurrency int32) {
	ns := key.Namespace
	revName := key.Name
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
	outstandingRequestsPerKey := make(map[types.NamespacedName]int32)
	// Contains the number of incoming requests in the current
	// reporting period, per key.
	incomingRequestsPerKey := make(map[types.NamespacedName]int32)

	for {
		select {
		case event := <-cr.reqCh:
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
		case <-cr.reportCh:
			for key, concurrency := range outstandingRequestsPerKey {
				if concurrency == 0 {
					delete(outstandingRequestsPerKey, key)
				} else {
					cr.reportToAutoscaler(key, concurrency, incomingRequestsPerKey[key])
				}
				cr.reportToMetricsBackend(key, concurrency)
			}

			incomingRequestsPerKey = make(map[types.NamespacedName]int32)
		case <-stopCh:
			return
		}
	}
}
