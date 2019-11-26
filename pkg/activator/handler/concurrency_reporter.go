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
	"context"
	"time"

	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
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
	statCh chan []autoscaler.StatMessage

	rl servinglisters.RevisionLister
	sr activator.StatsReporter
}

// NewConcurrencyReporter creates a ConcurrencyReporter which listens to incoming
// ReqEvents on reqCh and ticks on reportCh and reports stats on statCh.
func NewConcurrencyReporter(ctx context.Context, podName string,
	reqCh chan ReqEvent, reportCh <-chan time.Time, statCh chan []autoscaler.StatMessage,
	sr activator.StatsReporter) *ConcurrencyReporter {
	return &ConcurrencyReporter{
		logger:   logging.FromContext(ctx),
		podName:  podName,
		reqCh:    reqCh,
		reportCh: reportCh,
		statCh:   statCh,
		rl:       revisioninformer.Get(ctx).Lister(),
		sr:       sr,
	}
}

func (cr *ConcurrencyReporter) reportToMetricsBackend(key types.NamespacedName, concurrency int64) {
	ns := key.Namespace
	revName := key.Name
	revision, err := cr.rl.Revisions(ns).Get(revName)
	if err != nil {
		cr.logger.Errorw("Error while getting revision", zap.Error(err))
		return
	}
	configurationName := revision.Labels[serving.ConfigurationLabelKey]
	serviceName := revision.Labels[serving.ServiceLabelKey]
	cr.sr.ReportRequestConcurrency(ns, serviceName, configurationName, revName, concurrency)
}

// Run runs until stopCh is closed and processes events on all incoming channels
func (cr *ConcurrencyReporter) Run(stopCh <-chan struct{}) {
	// Contains the number of in-flight requests per-key
	outstandingRequestsPerKey := make(map[types.NamespacedName]int64)
	// Contains the number of incoming requests in the current
	// reporting period, per key.
	incomingRequestsPerKey := make(map[types.NamespacedName]int64)

	for {
		select {
		case event := <-cr.reqCh:
			switch event.EventType {
			case ReqIn:
				incomingRequestsPerKey[event.Key]++

				// Report the first request for a key immediately.
				if _, ok := outstandingRequestsPerKey[event.Key]; !ok {
					cr.statCh <- []autoscaler.StatMessage{{
						Key: event.Key,
						Stat: autoscaler.Stat{
							// Stat time is unset by design. The receiver will set the time.
							PodName:                   cr.podName,
							AverageConcurrentRequests: 1,
							RequestCount:              float64(incomingRequestsPerKey[event.Key]),
						},
					}}
				}
				outstandingRequestsPerKey[event.Key]++
			case ReqOut:
				outstandingRequestsPerKey[event.Key]--
			}
		case <-cr.reportCh:
			messages := make([]autoscaler.StatMessage, 0, len(outstandingRequestsPerKey))
			for key, concurrency := range outstandingRequestsPerKey {
				if concurrency == 0 {
					delete(outstandingRequestsPerKey, key)
				} else {
					messages = append(messages, autoscaler.StatMessage{
						Key: key,
						Stat: autoscaler.Stat{
							// Stat time is unset by design. The receiver will set the time.
							PodName:                   cr.podName,
							AverageConcurrentRequests: float64(concurrency),
							RequestCount:              float64(incomingRequestsPerKey[key]),
						},
					})
				}
				cr.reportToMetricsBackend(key, concurrency)
			}
			cr.statCh <- messages

			incomingRequestsPerKey = make(map[types.NamespacedName]int64)
		case <-stopCh:
			return
		}
	}
}
