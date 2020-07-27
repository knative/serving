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
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/activator/util"
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

	// Stat reporting channel
	statCh chan []asmetrics.StatMessage

	rl servinglisters.RevisionLister

	mux sync.RWMutex
	// This map holds the concurrency and request count accounting across revisions.
	stats map[types.NamespacedName]*network.RequestStats
	// This map holds whether during this reporting period we reported "first" request
	// for the revision. Our reporting period is 1s, so there is a high chance that
	// they will end up in the same metrics bucket and values in the same bucket are
	// summed.
	// This is important because for small concurrencies, e.g. 1, autoscaler might cause
	// noticeable overprovisioning.
	reportedFirstRequest map[types.NamespacedName]float64
}

// NewConcurrencyReporter creates a ConcurrencyReporter which listens to incoming
// ReqEvents on reqCh and ticks on reportCh and reports stats on statCh.
func NewConcurrencyReporter(ctx context.Context, podName string, statCh chan []asmetrics.StatMessage) *ConcurrencyReporter {
	return &ConcurrencyReporter{
		logger:  logging.FromContext(ctx),
		podName: podName,
		statCh:  statCh,
		rl:      revisioninformer.Get(ctx).Lister(),

		stats:                make(map[types.NamespacedName]*network.RequestStats),
		reportedFirstRequest: make(map[types.NamespacedName]float64),
	}
}

// handleEvent handles request events (in, out) and updates the respective stats.
func (cr *ConcurrencyReporter) handleEvent(event network.ReqEvent) {
	stats, msg := cr.getOrCreateStat(event)
	if msg != nil {
		cr.statCh <- []asmetrics.StatMessage{*msg}
	}
	stats.HandleEvent(event)
}

// getOrCreateStat gets a stat from the state if present.
// If absent it creates a new one and returns it, potentially returning a StatMessage too
// to trigger an immediate scale-from-0.
func (cr *ConcurrencyReporter) getOrCreateStat(event network.ReqEvent) (*network.RequestStats, *asmetrics.StatMessage) {
	stat := func() *network.RequestStats {
		cr.mux.RLock()
		defer cr.mux.RUnlock()
		return cr.stats[event.Key]
	}()
	if stat != nil {
		return stat, nil
	}

	// Doubly checked locking.
	cr.mux.Lock()
	defer cr.mux.Unlock()

	stat = cr.stats[event.Key]
	if stat != nil {
		return stat, nil
	}

	stat = network.NewRequestStats(event.Time)
	cr.stats[event.Key] = stat

	cr.reportedFirstRequest[event.Key] = 1
	return stat, &asmetrics.StatMessage{
		Key: event.Key,
		Stat: asmetrics.Stat{
			PodName:                   cr.podName,
			AverageConcurrentRequests: 1,
			// The way the checks are written, this cannot ever be
			// anything else but 1. The stats map key is only deleted
			// after a reporting period, so we see this code path at most
			// once per period.
			RequestCount: 1,
		},
	}
}

// report cuts a report from all collected statistics and sends the respective messages
// via the statsCh and reports the concurrency metrics to prometheus.
func (cr *ConcurrencyReporter) report(now time.Time) []asmetrics.StatMessage {
	cr.mux.Lock()
	defer cr.mux.Unlock()

	messages := make([]asmetrics.StatMessage, 0, len(cr.stats))
	for key, stat := range cr.stats {
		report := stat.Report(now)
		firstAdj := cr.reportedFirstRequest[key]

		// This is only 0 if we have seen no activity for the entire reporting
		// period at all.
		if report.AverageConcurrency == 0 {
			delete(cr.stats, key)
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
				PodName:                   cr.podName,
				AverageConcurrentRequests: adjustedConcurrency,
				RequestCount:              adjustedCount,
			},
		})
	}

	// We've now accounted for any cases where we immediately reported seeing the
	// first request for a revision in handleEvent by subtracting them from this
	// report.
	for k := range cr.reportedFirstRequest {
		delete(cr.reportedFirstRequest, k)
	}

	return messages
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
	for {
		select {
		case now := <-reportCh:
			msgs := cr.report(now)
			for _, msg := range msgs {
				cr.reportToMetricsBackend(msg.Key, msg.Stat.AverageConcurrentRequests)
			}
			if len(msgs) > 0 {
				cr.statCh <- msgs
			}
		case <-stopCh:
			return
		}
	}
}

// Handler returns a handler that records requests coming in/being finished in the stats
// machinery.
func (cr *ConcurrencyReporter) Handler(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		revisionKey := util.RevIDFrom(r.Context())
		cr.handleEvent(network.ReqEvent{Key: revisionKey, Type: network.ReqIn, Time: time.Now()})
		defer func() {
			cr.handleEvent(network.ReqEvent{Key: revisionKey, Type: network.ReqOut, Time: time.Now()})
		}()

		next.ServeHTTP(w, r)
	}
}
