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
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/metrics"
)

const reportInterval = time.Second

// ConcurrencyReporter reports stats based on incoming requests and ticks.
type ConcurrencyReporter struct {
	logger  *zap.SugaredLogger
	podName string

	// Ticks with every request arrived/completed respectively
	reqCh chan ReqEvent
	// Stat reporting channel
	statCh chan []asmetrics.StatMessage

	rl servinglisters.RevisionLister
}

// NewConcurrencyReporter creates a ConcurrencyReporter which listens to incoming
// ReqEvents on reqCh and ticks on reportCh and reports stats on statCh.
func NewConcurrencyReporter(ctx context.Context, podName string,
	reqCh chan ReqEvent, statCh chan []asmetrics.StatMessage) *ConcurrencyReporter {
	return &ConcurrencyReporter{
		logger:  logging.FromContext(ctx),
		podName: podName,
		reqCh:   reqCh,
		statCh:  statCh,
		rl:      revisioninformer.Get(ctx).Lister(),
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

	reporterCtx, _ := metrics.PodRevisionContext(cr.podName, activator.Name, ns, serviceName, configurationName, revName)
	pkgmetrics.RecordBatch(reporterCtx, requestConcurrencyM.M(concurrency))
}

// Run runs until stopCh is closed and processes events on all incoming channels.
func (cr *ConcurrencyReporter) Run(stopCh <-chan struct{}) {
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()
	cr.run(stopCh, ticker.C)
}

func (cr *ConcurrencyReporter) run(stopCh <-chan struct{}, reportCh <-chan time.Time) {
	// Contains the number of in-flight requests per-key
	outstandingRequestsPerKey := make(map[types.NamespacedName]int64)
	// Contains the number of incoming requests in the current
	// reporting period, per key.
	incomingRequestsPerKey := make(map[types.NamespacedName]float64)
	// This map holds whether during this reporting period we reported "first" request
	// for the revision. Our reporting period is 1s, so there is a high chance that
	// they will end up in the same metrics bucket.
	// This is important because for small concurrencies, e.g. 1,
	// autoscaler might cause noticeable overprovisioning.
	reportedFirstRequest := make(map[types.NamespacedName]int64)

	for {
		select {
		case event := <-cr.reqCh:
			switch event.EventType {
			case ReqIn:
				incomingRequestsPerKey[event.Key]++

				// Report the first request for a key immediately.
				if _, ok := outstandingRequestsPerKey[event.Key]; !ok {
					reportedFirstRequest[event.Key] = 1.
					cr.statCh <- []asmetrics.StatMessage{{
						Key: event.Key,
						Stat: asmetrics.Stat{
							// Stat time is unset by design. The receiver will set the time.
							PodName:                   cr.podName,
							AverageConcurrentRequests: 1,
							RequestCount:              incomingRequestsPerKey[event.Key],
						},
					}}
				}
				outstandingRequestsPerKey[event.Key]++
			case ReqOut:
				outstandingRequestsPerKey[event.Key]--
			}
		case <-reportCh:
			messages := make([]asmetrics.StatMessage, 0, len(outstandingRequestsPerKey))
			for key, concurrency := range outstandingRequestsPerKey {
				if concurrency == 0 {
					delete(outstandingRequestsPerKey, key)
				} else {
					messages = append(messages, asmetrics.StatMessage{
						Key: key,
						Stat: asmetrics.Stat{
							// Stat time is unset by design. The receiver will set the time.
							PodName: cr.podName,
							// Subtract the request we already reported when first seeing the revision.
							AverageConcurrentRequests: float64(concurrency - reportedFirstRequest[key]),
							RequestCount:              incomingRequestsPerKey[key],
						},
					})
				}
				cr.reportToMetricsBackend(key, concurrency)
			}
			cr.statCh <- messages

			incomingRequestsPerKey = make(map[types.NamespacedName]float64)
			reportedFirstRequest = make(map[types.NamespacedName]int64)
		case <-stopCh:
			return
		}
	}
}
