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
	"math"
	"time"

	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/network"
)

const reportInterval = time.Second

// ConcurrencyReporter reports stats based on incoming requests and ticks.
type ConcurrencyReporter struct {
	logger  *zap.SugaredLogger
	podName string

	// Ticks with every request arrived/completed respectively
	reqCh chan network.ReqEvent
	// Stat reporting channel
	statCh chan []asmetrics.StatMessage

	rl servinglisters.RevisionLister
}

// NewConcurrencyReporter creates a ConcurrencyReporter which listens to incoming
// ReqEvents on reqCh and ticks on reportCh and reports stats on statCh.
func NewConcurrencyReporter(ctx context.Context, podName string,
	reqCh chan network.ReqEvent, statCh chan []asmetrics.StatMessage) *ConcurrencyReporter {
	return &ConcurrencyReporter{
		logger:  logging.FromContext(ctx),
		podName: podName,
		reqCh:   reqCh,
		statCh:  statCh,
		rl:      revisioninformer.Get(ctx).Lister(),
	}
}

func (cr *ConcurrencyReporter) reportToMetricsBackend(key types.NamespacedName, concurrency float64) {
	ns := key.Namespace
	revName := key.Name
	revision, err := cr.rl.Revisions(ns).Get(revName)
	if err != nil {
		cr.logger.Errorw("Error while getting revision", zap.Any("revID", key), zap.Error(err))
		return
	}
	configurationName := revision.Labels[serving.ConfigurationLabelKey]
	serviceName := revision.Labels[serving.ServiceLabelKey]

	reporterCtx, _ := metrics.PodRevisionContext(cr.podName, activator.Name, ns, serviceName, configurationName, revName)
	pkgmetrics.Record(reporterCtx, requestConcurrencyM.M(concurrency))
}

// Run runs until stopCh is closed and processes events on all incoming channels.
func (cr *ConcurrencyReporter) Run(stopCh <-chan struct{}) {
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()
	cr.run(stopCh, ticker.C)
}

func (cr *ConcurrencyReporter) run(stopCh <-chan struct{}, reportCh <-chan time.Time) {
	// This map holds the concurrency and request count accounting across revisions.
	stats := make(map[types.NamespacedName]*network.RequestStats)
	// This map holds whether during this reporting period we reported "first" request
	// for the revision. Our reporting period is 1s, so there is a high chance that
	// they will end up in the same metrics bucket and values in the same bucket are
	// summed.
	// This is important because for small concurrencies, e.g. 1, autoscaler might cause
	// noticeable overprovisioning.
	reportedFirstRequest := make(map[types.NamespacedName]float64)

	for {
		select {
		case event := <-cr.reqCh:
			stat := stats[event.Key]
			if stat == nil {
				stat = network.NewRequestStats(event.Time)
				stats[event.Key] = stat

				// Only generate a from 0 event if this is an incoming request.
				if event.Type == network.ReqIn {
					reportedFirstRequest[event.Key] = 1
					cr.statCh <- []asmetrics.StatMessage{{
						Key: event.Key,
						Stat: asmetrics.Stat{
							// Stat time is unset by design. Will be set by receiver.
							PodName:                   cr.podName,
							AverageConcurrentRequests: 1,
							// The way the check above is written, this cannot ever be
							// anything else but 1. The stats map key is only deleted
							// after a reporting period, so we see this code path at most
							// once per period.
							RequestCount: 1,
						},
					}}
				}
			}

			stat.HandleEvent(event)
		case now := <-reportCh:
			messages := make([]asmetrics.StatMessage, 0, len(stats))
			for key, stat := range stats {
				report := stat.Report(now)
				firstAdj := reportedFirstRequest[key]

				// This is only 0 if we have seen no activity for the entire reporting
				// period at all.
				if report.AverageConcurrency == 0 {
					delete(stats, key)
				}

				// Subtract the request we already reported when first seeing the
				// revision. We report a min of 0 here because the initial report is
				// always a concurrency of 1 and the actual concurrency reported over
				// the reporting period might be < 1.
				adjustedConcurrency := math.Max(report.AverageConcurrency-firstAdj, 0)
				adjustedCount := report.RequestCount - firstAdj
				messages = append(messages, asmetrics.StatMessage{
					Key: key,
					Stat: asmetrics.Stat{
						// Stat time is unset by design. The receiver will set the time.
						PodName:                   cr.podName,
						AverageConcurrentRequests: adjustedConcurrency,
						RequestCount:              adjustedCount,
					},
				})
				cr.reportToMetricsBackend(key, report.AverageConcurrency)
			}
			if len(messages) > 0 {
				cr.statCh <- messages
			}

			reportedFirstRequest = make(map[types.NamespacedName]float64)
		case <-stopCh:
			return
		}
	}
}
