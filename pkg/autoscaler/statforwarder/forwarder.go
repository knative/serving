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

package statforwarder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/util/workqueue"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/network"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	// The port on which autoscaler WebSocket server listens.
	autoscalerPort     = 8080
	autoscalerPortName = "http"
	retryTimeout       = 3 * time.Second
	retryInterval      = 100 * time.Millisecond

	// Retry at most 15 seconds to process a stat.
	maxProcessingRetry = 30
	// retryProcessingInterval is kept for backward compatibility but no longer used.
	// Use rateLimiter instead.
	retryProcessingInterval = 500 * time.Millisecond

	// Rate limiting configuration for retries.
	// fastRetryDelay is used for the first maxFastRetryAttempts retries.
	fastRetryDelay = 100 * time.Millisecond
	// slowRetryDelay is used after maxFastRetryAttempts retries.
	slowRetryDelay = 500 * time.Millisecond
	// maxFastRetryAttempts is the number of fast retries before switching to slow retries.
	maxFastRetryAttempts = 5
)

var svcURLSuffix = fmt.Sprintf("svc.%s:%d", network.GetClusterDomainName(), autoscalerPort)

// statProcessor is a function to process a single StatMessage.
type statProcessor func(sm asmetrics.StatMessage)

type stat struct {
	sm    asmetrics.StatMessage
	retry int
}

// Forwarder does the following things:
//  1. Watches the change of Leases for Autoscaler buckets. Stores the
//     Lease -> IP mapping.
//  2. Creates/updates the corresponding K8S Service and Endpoints.
//  3. Can be used to forward the metrics owned by a bucket based on
//     the holder IP.
type Forwarder struct {
	logger *zap.SugaredLogger
	// bs is the BucketSet including all Autoscaler buckets.
	bs *hash.BucketSet

	// processorsLock is the lock for processors.
	processorsLock sync.RWMutex
	processors     map[string]bucketProcessor
	// Used to capture asynchronous processes for re-enqueuing to be waited
	// on when shutting down.
	retryWg sync.WaitGroup
	// Used to capture asynchronous processes for stats to be waited
	// on when shutting down.
	processingWg sync.WaitGroup

	statCh chan stat
	stopCh chan struct{}

	// rateLimiter controls the retry backoff strategy using fast/slow retry delays.
	rateLimiter workqueue.TypedRateLimiter[string]
}

// New creates a new Forwarder.
// This must be configured with a mechanism for setting up its "processors",
// such as LeaseBasedProcessor or StatefulSetBasedProcessor, which correlates
// with the mechanism of leader election being used.
func New(ctx context.Context, bs *hash.BucketSet) *Forwarder {
	bkts := bs.Buckets()
	f := &Forwarder{
		logger:      logging.FromContext(ctx),
		bs:          bs,
		processors:  make(map[string]bucketProcessor, len(bkts)),
		statCh:      make(chan stat, 1000),
		stopCh:      make(chan struct{}),
		rateLimiter: workqueue.NewTypedItemFastSlowRateLimiter[string](fastRetryDelay, slowRetryDelay, maxFastRetryAttempts),
	}

	f.processingWg.Add(1)
	go f.process()

	return f
}

func (f *Forwarder) getProcessor(bkt string) bucketProcessor {
	f.processorsLock.RLock()
	defer f.processorsLock.RUnlock()
	return f.processors[bkt]
}

func (f *Forwarder) setProcessor(bkt string, p bucketProcessor) {
	f.processorsLock.Lock()
	defer f.processorsLock.Unlock()
	f.processors[bkt] = p
}

// Process enqueues the given Stat for processing asynchronously.
// It calls Forwarder.accept if the pod where this Forwarder is running is the owner
// of the given StatMessage. Otherwise it forwards the given StatMessage to the right
// owner pod. It will retry if any error happens during the processing.
func (f *Forwarder) Process(sm asmetrics.StatMessage) {
	f.statCh <- stat{sm: sm, retry: 0}
}

func (f *Forwarder) process() {
	defer func() {
		f.retryWg.Wait()
		f.processingWg.Done()
	}()

	for {
		select {
		case <-f.stopCh:
			return
		case s := <-f.statCh:
			rev := s.sm.Key.String()
			l := f.logger.With(zap.String(logkey.Key, rev))
			bkt := f.bs.Owner(rev)

			p := f.getProcessor(bkt)
			if p == nil {
				l.Warn("Can't find the owner for Revision bucket: ", bkt)
				f.maybeRetry(l, s)
				continue
			}

			if err := p.process(s.sm); err != nil {
				l.Errorw("Error while processing stat", zap.Error(err))
				f.maybeRetry(l, s)
			} else {
				// Successfully processed, forget the retry state for this stat.
				f.rateLimiter.Forget(rev)
			}
		}
	}
}

func (f *Forwarder) maybeRetry(logger *zap.SugaredLogger, s stat) {
	rev := s.sm.Key.String()

	// Check current retry count before calling When (which increments the count).
	numRequeues := f.rateLimiter.NumRequeues(rev)
	if numRequeues >= maxProcessingRetry {
		logger.Warnf("Exceeding max retries (%d). Dropping the stat.", maxProcessingRetry)
		// Clean up the rate limiter state for this stat.
		f.rateLimiter.Forget(rev)
		return
	}

	// Get the retry delay from the rate limiter (fast for initial retries, slow after).
	// This will increment the internal retry count.
	retryDelay := f.rateLimiter.When(rev)

	f.retryWg.Add(1)
	go func() {
		defer f.retryWg.Done()
		time.Sleep(retryDelay)
		logger.Debugf("Enqueuing stat for retry (attempt %d, delay %v).", numRequeues+1, retryDelay)
		// Increment retry count for tracking (though rate limiter also tracks this).
		s.retry++
		f.statCh <- s
	}()
}

// Cancel is the function to call when terminating a Forwarder.
func (f *Forwarder) Cancel() {
	// Tell process go-runtine to stop.
	close(f.stopCh)

	f.processorsLock.RLock()
	defer f.processorsLock.RUnlock()
	for _, p := range f.processors {
		if p != nil {
			p.shutdown()
		}
	}

	f.processingWg.Wait()
	close(f.statCh)
}

// IsBucketOwner returns true if this Autoscaler pod is the owner of the given bucket.
func (f *Forwarder) IsBucketOwner(bkt string) bool {
	_, owned := f.getProcessor(bkt).(*localProcessor)
	return owned
}
