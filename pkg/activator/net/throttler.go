/*
Copyright 2019 The Knative Authors

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

package net

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/maps"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/sets"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/tools/cache"

	pkgnet "knative.dev/networking/pkg/apis/networking"
	netcfg "knative.dev/networking/pkg/config"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
)

const (
	// The number of requests that are queued on the breaker before the 503s are sent.
	// The value must be adjusted depending on the actual production requirements.
	// This value is used both for the breaker in revisionThrottler (throttling
	// across the entire revision), and for the individual podTracker breakers.
	breakerQueueDepth = 10000

	// The revisionThrottler breaker's concurrency increases up to this value as
	// new endpoints show up. We need to set some value here since the breaker
	// requires an explicit buffer size (it's backed by a chan struct{}), but
	// queue.MaxBreakerCapacity is math.MaxInt32.
	revisionMaxConcurrency = queue.MaxBreakerCapacity

	// Maximum time a pod can stay in draining state before being forcefully removed
	// This allows long-running requests to complete gracefully
	maxDrainingDuration = 1 * time.Hour

	// Pod ready check timeout used to verify queue-proxy health
	podReadyCheckTimeout = 5000 * time.Millisecond

	// QPFreshnessWindow - Queue-proxy data fresher than this overrides K8s informer
	// If QP spoke within this window, trust QP state over informer
	QPFreshnessWindow = 30 * time.Second

	// QPStalenessThreshold - Queue-proxy older than this, trust K8s instead
	// If QP has been silent this long, it's likely dead and informer is authoritative
	QPStalenessThreshold = 60 * time.Second
)

func newPodTracker(dest string, revisionID types.NamespacedName, b breaker) *podTracker {
	tracker := &podTracker{
		id:         string(uuid.NewUUID()),
		createdAt:  time.Now().UnixMicro(),
		dest:       dest,
		revisionID: revisionID,
		b:          b,
	}
	tracker.state.Store(uint32(podPending))
	tracker.refCount.Store(0)
	tracker.drainingStartTime.Store(0)
	tracker.weight.Store(0)
	tracker.lastQPUpdate.Store(0)
	tracker.lastQPState.Store("")
	tracker.decreaseWeight = func() {
		if tracker.weight.Load() > 0 {
			tracker.weight.Add(^uint32(0))
		}
	}

	return tracker
}

type podState uint32

const (
	podHealthy podState = iota
	podDraining
	podRemoved
	podPending // Waiting for ready signal (not viable for routing)
)

type podTracker struct {
	id         string
	createdAt  int64
	dest       string
	b          breaker
	revisionID types.NamespacedName

	// State machine for pod health transitions
	state atomic.Uint32 // Uses podState constants
	// Reference count for in-flight requests to support graceful draining
	refCount atomic.Uint64
	// Unix timestamp when the pod entered draining state
	drainingStartTime atomic.Int64

	// Queue-proxy push tracking (for trust hierarchy over K8s informer)
	// Unix timestamp of last queue-proxy push update (0 if never received)
	lastQPUpdate atomic.Int64
	// Last queue-proxy event type: "startup", "ready", "not-ready", "draining"
	lastQPState atomic.Value

	// weight is used for LB policy implementations.
	weight atomic.Uint32
	// decreaseWeight is an allocation optimization for the randomChoice2 policy.
	decreaseWeight func()
}

// Reference counting helper methods
func (p *podTracker) addRef() {
	p.refCount.Add(1)
}

func (p *podTracker) releaseRef() {
	current := p.refCount.Load()
	if current == 0 {
		// This should never happen in correct code
		if logger := logging.FromContext(context.Background()); logger != nil {
			logger.Errorf("BUG: Attempted to release ref on pod %s with zero refcount", p.dest)
		}
		return
	}
	p.refCount.Add(^uint64(0))
}

func (p *podTracker) getRefCount() uint64 {
	return p.refCount.Load()
}

func (p *podTracker) tryDrain() bool {
	if p.state.CompareAndSwap(uint32(podHealthy), uint32(podDraining)) {
		p.drainingStartTime.Store(time.Now().Unix())
		return true
	}
	return false
}

func (p *podTracker) increaseWeight() {
	p.weight.Add(1)
}

func (p *podTracker) getWeight() uint32 {
	return p.weight.Load()
}

func (p *podTracker) String() string {
	if p == nil {
		return "<nil>"
	}
	return p.dest
}

func (p *podTracker) Capacity() uint64 {
	if p.b == nil {
		return 1
	}
	return p.b.Capacity()
}

func (p *podTracker) Pending() int {
	if p.b == nil {
		return 0
	}
	return p.b.Pending()
}

func (p *podTracker) InFlight() uint64 {
	if p.b == nil {
		return 0
	}
	return p.b.InFlight()
}

func (p *podTracker) UpdateConcurrency(c int) {
	if p.b == nil {
		return
	}
	p.b.UpdateConcurrency(c)
}

// Reserve attempts to reserve capacity on this pod.
// Returns false if the pod is unhealthy, preventing new requests from being routed to it.
func (p *podTracker) Reserve(ctx context.Context) (func(), bool) {
	defer func() {
		if r := recover(); r != nil {
			if logger := logging.FromContext(ctx); logger != nil {
				logger.Errorf("Panic in podTracker.Reserve for pod %s: %v", p.dest, r)
			}
			p.releaseRef()
			panic(r)
		}
	}()

	// Increment ref count before checking state to prevent race with pod removal
	p.addRef()

	state := podState(p.state.Load())
	// ONLY healthy pods can accept new requests
	// podPending pods are excluded from routing until explicitly promoted to healthy
	// This prevents premature health check failures and quarantine churn
	if state != podHealthy {
		p.releaseRef()
		return nil, false
	}

	if p.b == nil {
		return func() {
			p.releaseRef()
		}, true
	}

	release, ok := p.b.Reserve(ctx)
	if !ok {
		p.releaseRef()
		return nil, false
	}

	// Return wrapped release function
	return func() {
		release()
		p.releaseRef()
	}, true
}

type breaker interface {
	Capacity() uint64
	Maybe(ctx context.Context, thunk func()) error
	UpdateConcurrency(int)
	Reserve(ctx context.Context) (func(), bool)
	Pending() int
	InFlight() uint64
}

type revisionThrottler struct {
	revID                types.NamespacedName
	containerConcurrency atomic.Uint32
	lbPolicy             atomic.Value // Store lbPolicy function atomically

	// These are used in slicing to infer which pods to assign
	// to this activator.
	numActivators atomic.Uint32
	// If -1, it is presumed that this activator should not receive requests
	// for the revision. But due to the system being distributed it might take
	// time for everything to propagate. Thus when this is -1 we assign all the
	// pod trackers.
	activatorIndex atomic.Int32
	protocol       string

	// Holds the current number of backends. This is used for when we get an activatorCount update and
	// therefore need to recalculate capacity
	backendCount atomic.Uint32 // Make atomic to prevent races

	// This is a breaker for the revision as a whole.
	breaker breaker

	// This will be non-empty when we're able to use pod addressing.
	podTrackers map[string]*podTracker

	// Effective trackers that are assigned to this Activator.
	// This is a subset of podTrackers.
	assignedTrackers []*podTracker

	// If we don't have a healthy clusterIPTracker this is set to nil, otherwise
	// it is the l4dest for this revision's private clusterIP.
	clusterIPTracker *podTracker

	// mux guards the "throttler state" which is the state we use during the
	// request path. This is: trackers, clusterIPDest.
	mux sync.RWMutex

	logger *zap.SugaredLogger
}

// validateLoadBalancingPolicy checks if the given policy is valid
func validateLoadBalancingPolicy(policy string) bool {
	validPolicies := map[string]bool{
		"random-choice-2":   true,
		"round-robin":       true,
		"least-connections": true,
		"first-available":   true,
	}
	return validPolicies[policy]
}

func pickLBPolicy(loadBalancerPolicy *string, _ map[string]string, containerConcurrency int, logger *zap.SugaredLogger) (lbPolicy, string) {
	// Honor explicit spec field first
	if loadBalancerPolicy != nil && *loadBalancerPolicy != "" {
		if !validateLoadBalancingPolicy(*loadBalancerPolicy) {
			logger.Errorf("Invalid load balancing policy %q, using defaults", *loadBalancerPolicy)
		} else {
			switch *loadBalancerPolicy {
			case "random-choice-2":
				return randomChoice2Policy, "random-choice-2"
			case "round-robin":
				return newRoundRobinPolicy(), "round-robin"
			case "least-connections":
				return leastConnectionsPolicy, "least-connections"
			case "first-available":
				return firstAvailableLBPolicy, "first-available"
			}
		}
	}
	// Fall back to containerConcurrency-based defaults
	switch {
	case containerConcurrency == 0:
		return randomChoice2Policy, "random-choice-2 (default for CC=0)"
	case containerConcurrency <= 3:
		return firstAvailableLBPolicy, "first-available (default for CC<=3)"
	default:
		return newRoundRobinPolicy(), "round-robin (default for CC>3)"
	}
}

func newRevisionThrottler(revID types.NamespacedName,
	loadBalancerPolicy *string,
	containerConcurrency int, proto string,
	breakerParams queue.BreakerParams,
	logger *zap.SugaredLogger,
) *revisionThrottler {
	logger = logger.With(zap.String(logkey.Key, revID.String()))
	var (
		revBreaker breaker
		lbp        lbPolicy
		lbpName    string
	)

	lbp, lbpName = pickLBPolicy(loadBalancerPolicy, nil, containerConcurrency, logger)
	logger.Debugf("Creating revision throttler with load balancing policy: %s, container concurrency: %d", lbpName, containerConcurrency)

	if containerConcurrency == 0 {
		revBreaker = newInfiniteBreaker(logger)
	} else {
		revBreaker = queue.NewBreaker(breakerParams)
	}
	t := &revisionThrottler{
		revID:       revID,
		breaker:     revBreaker,
		logger:      logger,
		protocol:    proto,
		podTrackers: make(map[string]*podTracker),
	}
	t.containerConcurrency.Store(uint32(containerConcurrency))
	t.lbPolicy.Store(lbp)

	// Start with unknown
	t.activatorIndex.Store(-1)
	return t
}

func noop() {}

// filterAvailableTrackers returns only healthy pods ready to serve traffic
// podPending pods are excluded until explicitly promoted to healthy
func (rt *revisionThrottler) filterAvailableTrackers(trackers []*podTracker) []*podTracker {
	available := make([]*podTracker, 0, len(trackers))

	for _, tracker := range trackers {
		if tracker != nil && podState(tracker.state.Load()) == podHealthy {
			available = append(available, tracker)
		}
	}

	return available
}

func (rt *revisionThrottler) acquireDest(ctx context.Context) (func(), *podTracker, bool) {
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	// Disabled clusterIP routing - always use pod routing
	// if rt.clusterIPTracker != nil {
	// 	return noop, rt.clusterIPTracker, true
	// }

	// Filter to only healthy pods (excludes pending, draining, removed)
	originalCount := len(rt.assignedTrackers)
	availableTrackers := rt.filterAvailableTrackers(rt.assignedTrackers)

	// Log availability issues - changed to DEBUG to avoid spam in re-enqueue loops
	if len(availableTrackers) == 0 && originalCount > 0 {
		rt.logger.Debugw("No available pods after filtering",
			"assigned", originalCount,
			"available", 0,
			"revision", rt.revID.String())
	} else if originalCount > 2 && len(availableTrackers) < originalCount/2 {
		rt.logger.Debugw("Many pods unavailable",
			"assigned", originalCount,
			"available", len(availableTrackers),
			"revision", rt.revID.String())
	}

	if len(availableTrackers) == 0 {
		return noop, nil, false
	}
	lbPolicy := rt.lbPolicy.Load().(lbPolicy)
	callback, pt := lbPolicy(ctx, availableTrackers)
	return callback, pt, false
}

func (rt *revisionThrottler) try(ctx context.Context, xRequestId string, function func(dest string, isClusterIP bool) error) error {
	// Start timing for proxy start latency and threshold tracking
	proxyStartTime := time.Now()

	// Timers to track threshold breaches
	warningTimer := time.NewTimer(4 * time.Second)
	errorTimer := time.NewTimer(60 * time.Second)
	criticalTimer := time.NewTimer(180 * time.Second)
	defer warningTimer.Stop()
	defer errorTimer.Stop()
	defer criticalTimer.Stop()

	// Channel to ensure shutdown - will be closed when routing completes (not proxy)
	thresholdChan := make(chan struct{})
	var thresholdChanClosed bool
	var thresholdMu sync.Mutex

	// Ensure we always close the channel on function exit
	defer func() {
		thresholdMu.Lock()
		if !thresholdChanClosed {
			close(thresholdChan)
			thresholdChanClosed = true
		}
		thresholdMu.Unlock()
	}()

	// Goroutine to track threshold breaches for routing/queuing time only
	go func() {
		for {
			select {
			case <-warningTimer.C:
				handler.RecordProxyQueueTimeWarning(ctx)
				rt.logger.Debugf("Request %s exceeded WARNING proxy queue time threshold (4s)", xRequestId)
			case <-errorTimer.C:
				handler.RecordProxyQueueTimeError(ctx)
				rt.logger.Debugf("Request %s exceeded ERROR proxy queue time threshold (60s)", xRequestId)
			case <-criticalTimer.C:
				handler.RecordProxyQueueTimeCritical(ctx)
				rt.logger.Debugf("Request %s exceeded CRITICAL proxy queue time threshold (3m)", xRequestId)
			case <-thresholdChan:
				// Routing completed, stop monitoring
				return
			case <-ctx.Done():
				// Context has terminated
				return
			}
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			rt.logger.Errorf("Panic in revisionThrottler.try for request %s: %v", xRequestId, r)
			panic(r)
		}
	}()

	// Record that this request is now pending for a podTracker
	handler.RecordPendingRequest(ctx)
	handler.RecordPendingRequestStart(ctx) // New counter-based metric
	defer func() {
		handler.RecordPendingRequestComplete(ctx)
		handler.RecordPendingRequestCompleted(ctx) // New counter-based metric
	}()

	var ret error
	reenqueueCount := 0
	var finalTracker string     // Store the final tracker destination
	var proxyDurationMs float64 // Store the proxy call duration

	// Retrying infinitely as long as we receive no dest. Outer semaphore and inner
	// pod capacity are not changed atomically, hence they can race each other. We
	// "reenqueue" requests should that happen.
	reenqueue := true
	for reenqueue {
		reenqueue = false
		if reenqueueCount > 0 {
			rt.logger.Debugw("Request retry attempt",
				"x-request-id", xRequestId,
				"retry", reenqueueCount,
				"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()))
		}

		rt.mux.RLock()
		assignedTrackers := rt.assignedTrackers
		rt.mux.RUnlock()
		if len(assignedTrackers) == 0 {
			rt.logger.Debugf("%s -> No Assigned trackers\n", xRequestId)
		}

		// Track revision-level breaker wait
		breakerWaitStart := time.Now()
		breakerCapacity := rt.breaker.Capacity()
		breakerPending := rt.breaker.Pending()
		breakerInflight := rt.breaker.InFlight()

		// Get diagnostic info about pod/tracker state before entering breaker
		rt.mux.RLock()
		totalPods := len(rt.podTrackers)
		assignedCount := len(rt.assignedTrackers)
		rt.mux.RUnlock()

		ai := rt.activatorIndex.Load()
		ac := rt.numActivators.Load()
		backends := rt.backendCount.Load()
		cc := rt.containerConcurrency.Load()

		// Log if we're about to wait with zero capacity - helps diagnose why
		if breakerCapacity == 0 {
			rt.logger.Warnw("Request blocked: revision has zero capacity",
				"x-request-id", xRequestId,
				"total-pods", totalPods,
				"assigned-trackers", assignedCount,
				"backends", backends,
				"activator-index", ai,
				"activator-count", ac,
				"container-concurrency", cc,
				"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()))
		} else if breakerCapacity < 10 && breakerPending > 0 {
			// TODO: Remove this diagnostic log after capacity lag investigation is complete
			// Log when capacity is low and requests are queued - helps diagnose why capacity
			// isn't increasing proportionally with pod count
			rt.logger.Warnw("Request entering breaker with low capacity and queued requests",
				"x-request-id", xRequestId,
				"capacity", breakerCapacity,
				"pending", breakerPending,
				"inflight", breakerInflight,
				"total-pods", totalPods,
				"assigned-trackers", assignedCount,
				"backends", backends,
				"activator-index", ai,
				"activator-count", ac,
				"container-concurrency", cc,
				"expected-capacity", int(cc)*assignedCount,
				"capacity-deficit", int(cc)*assignedCount-int(uint64(breakerCapacity)), //nolint:gosec // breakerCapacity fits in int
				"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()))
		}

		if err := rt.breaker.Maybe(ctx, func() {
			breakerWaitMs := float64(time.Since(breakerWaitStart).Milliseconds())
			if breakerWaitMs > 100 { // Log if waited more than 100ms for revision capacity
				rt.logger.Warnw("Request waited for revision breaker capacity",
					"x-request-id", xRequestId,
					"wait-ms", breakerWaitMs,
					"capacity", breakerCapacity,
					"pending", breakerPending,
					"inflight", breakerInflight)
			}

			// Track pod selection time
			acquireStart := time.Now()
			callback, tracker, isClusterIP := rt.acquireDest(ctx)
			acquireMs := float64(time.Since(acquireStart).Milliseconds())
			if acquireMs > 50 { // Log if pod selection took >50ms
				rt.logger.Warnw("Slow pod selection",
					"x-request-id", xRequestId,
					"acquire-ms", acquireMs,
					"tracker-found", tracker != nil)
			}
			if tracker == nil {
				// Check state of all assigned pods
				rt.mux.RLock()
				assignedCount := len(rt.assignedTrackers)
				pendingCount := 0
				for _, t := range rt.assignedTrackers {
					if t != nil {
						state := podState(t.state.Load())
						if state == podPending {
							pendingCount++
						}
					}
				}
				rt.mux.RUnlock()

				// Log if all pods are pending - rate limit to avoid spam
				if pendingCount == assignedCount && assignedCount > 0 {
					shouldLog := reenqueueCount == 0 || reenqueueCount%100 == 0
					if shouldLog {
						rt.logger.Warnw("all pods pending (starting up); re-enqueue",
							"x-request-id", xRequestId,
							"pending", pendingCount,
							"assigned", assignedCount,
							"reenqueueCount", reenqueueCount,
							"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()),
						)
					}
					reenqueue = true
					reenqueueCount++
					// Small jittered backoff to avoid tight loop/herd effects
					select {
					case <-time.After(20*time.Millisecond + time.Duration(time.Now().UnixNano()%20_000_000)):
					case <-ctx.Done():
					}
					return
				}

				// This can happen if individual requests raced each other or if pod
				// capacity was decreased after passing the outer semaphore.
				reenqueue = true
				reenqueueCount++
				rt.logger.Debugw("Failed to acquire tracker; re-enqueue",
					"x-request-id", xRequestId,
					"pending", pendingCount,
					"assigned", assignedCount,
					"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()))
				return
			}
			trackerId := tracker.id

			// CRITICAL: Validate this tracker belongs to the correct revision
			if tracker.revisionID != rt.revID {
				rt.logger.Errorw("CRITICAL BUG: Acquired tracker from wrong revision - IP reuse detected!",
					"x-request-id", xRequestId,
					"expected-revision", rt.revID.String(),
					"tracker-revision", tracker.revisionID.String(),
					"dest", tracker.dest,
					"tracker-id", trackerId,
					"tracker-created-at", tracker.createdAt)
				// Re-enqueue to try a different tracker
				reenqueue = true
				reenqueueCount++
				return
			}

			rt.logger.Debugw("Acquired pod tracker for request",
				"x-request-id", xRequestId,
				"tracker-id", trackerId,
				"dest", tracker.dest,
				"revision", rt.revID.String(),
				"created-at", tracker.createdAt,
				"state", podState(tracker.state.Load()),
				"reenqueue-count", reenqueueCount)
			rt.logger.Debugf("Tracker %s Breaker State: capacity: %d, inflight: %d, pending: %d", trackerId, tracker.Capacity(), tracker.InFlight(), tracker.Pending())
			defer func() {
				callback()
				rt.logger.Debugf("%s -> %s breaker release semaphore\n", xRequestId, trackerId)
			}()

			// Record proxy start latency (successful tracker acquisition)
			proxyStartLatencyMs := float64(time.Since(proxyStartTime).Nanoseconds()) / 1e6
			handler.RecordProxyStartLatency(ctx, proxyStartLatencyMs)

			// Stop threshold monitoring now that we've acquired a tracker and are starting to proxy
			// This should only monitor routing/queuing time, not the actual proxy duration
			thresholdMu.Lock()
			if !thresholdChanClosed {
				close(thresholdChan)
				thresholdChanClosed = true
			}
			thresholdMu.Unlock()

			// Log if routing took too long
			if proxyStartLatencyMs > 1000 { // More than 1 second
				rt.logger.Warnw("Slow routing decision",
					"x-request-id", xRequestId,
					"dest", tracker.dest,
					"latency-ms", proxyStartLatencyMs,
					"reenqueue-count", reenqueueCount)
			}

			// Store the final tracker used
			finalTracker = tracker.dest

			// Time the actual proxy call
			proxyCallStart := time.Now()
			ret = function(tracker.dest, isClusterIP)
			proxyDurationMs = float64(time.Since(proxyCallStart).Milliseconds())

			// Request completed - no state transitions needed
			// Trust that backend manager and QP handle health state
		}); err != nil {
			return err
		}
		if reenqueue {
			rt.logger.Debugw("Request will be re-queued",
				"x-request-id", xRequestId,
				"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()))
		}
	}

	// Log final routing summary if it took significant time or had retries
	totalMs := float64(time.Since(proxyStartTime).Milliseconds())
	if totalMs > 500 || reenqueueCount > 0 { // Log if >500ms or had retries
		rt.logger.Infow("Request routing completed",
			"x-request-id", xRequestId,
			"dest", finalTracker,
			"total-ms", totalMs,
			"routing-ms", totalMs-proxyDurationMs,
			"proxy-ms", proxyDurationMs,
			"retries", reenqueueCount,
			"success", ret == nil)
	}

	return ret
}

func (rt *revisionThrottler) calculateCapacity(backendCount, numTrackers, activatorCount int) int {
	var targetCapacity int
	if numTrackers > 0 {
		// Capacity is computed based off of number of trackers,
		// when using pod direct routing.
		// We use number of assignedTrackers (numTrackers) for calculation
		// since assignedTrackers means activator's capacity
		targetCapacity = int(rt.containerConcurrency.Load()) * numTrackers
	} else {
		// Capacity is computed off of number of ready backends,
		// when we are using clusterIP routing.
		targetCapacity = int(rt.containerConcurrency.Load()) * backendCount
		if targetCapacity > 0 {
			targetCapacity = minOneOrValue(targetCapacity / minOneOrValue(activatorCount))
		}
	}

	if (backendCount > 0) && (rt.containerConcurrency.Load() == 0 || targetCapacity > revisionMaxConcurrency) {
		// If cc==0, we need to pick a number, but it does not matter, since
		// infinite breaker will dole out as many tokens as it can.
		// For cc>0 we clamp targetCapacity to maxConcurrency because the backing
		// breaker requires some limit (it's backed by a chan struct{}), but the
		// limit is math.MaxInt32 so in practice this should never be a real limit.
		targetCapacity = revisionMaxConcurrency
	}

	return targetCapacity
}

// This makes sure we reset the capacity to the CC, since the pod
// might be reassigned to be exclusively used.
func (rt *revisionThrottler) resetTrackers() {
	cc := int(rt.containerConcurrency.Load())
	if cc <= 0 {
		return
	}

	// Update trackers directly under lock to avoid race condition
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	for _, t := range rt.podTrackers {
		if t != nil {
			t.UpdateConcurrency(cc)
		}
	}
}

// updateCapacity updates the capacity of the throttler and recomputes
// the assigned trackers to the Activator instance.
// Currently updateCapacity is ensured to be invoked from a single go routine
// and this does not synchronize
func (rt *revisionThrottler) updateCapacity(backendCount int) {
	// We have to make assignments on each updateCapacity, since if number
	// of activators changes, then we need to rebalance the assignedTrackers.
	ac, ai := int(rt.numActivators.Load()), int(rt.activatorIndex.Load())
	numTrackers := func() int {
		// We need to read podTrackers under lock for race safety, even though
		// updateCapacity is guaranteed to be executed by a single goroutine.
		// Other goroutines like resetTrackers may also read podTrackers.

		rt.mux.RLock()
		// We're using cluster IP.
		if rt.clusterIPTracker != nil {
			rt.mux.RUnlock()
			return 0
		}

		var assigned []*podTracker
		if rt.containerConcurrency.Load() > 0 {
			rt.mux.RUnlock() // Release lock before calling resetTrackers
			rt.resetTrackers()
			rt.mux.RLock() // Re-acquire for assignSlice
			assigned = assignSlice(rt.podTrackers, ai, ac)
		} else {
			assigned = maps.Values(rt.podTrackers)
		}
		// TODO: Remove this diagnostic log after capacity lag investigation is complete
		// Capture total pods while still holding lock to avoid race
		totalPodsSnapshot := len(rt.podTrackers)
		rt.mux.RUnlock()

		rt.logger.Debugf("Trackers %d/%d: assignment: %v", ai, ac, assigned)

		// Log assignment details to diagnose why assigned count differs from total pods
		if len(assigned) != totalPodsSnapshot {
			rt.logger.Debugw("Pod assignment mismatch detected",
				"total-pods-in-map", totalPodsSnapshot,
				"assigned-to-this-activator", len(assigned),
				"activator-index", ai,
				"activator-count", ac)
		}

		// Sort, so we get more or less stable results.
		sort.Slice(assigned, func(i, j int) bool {
			return assigned[i].dest < assigned[j].dest
		})

		// The actual write out of the assigned trackers has to be under lock.
		rt.mux.Lock()
		rt.assignedTrackers = assigned
		rt.mux.Unlock()
		return len(assigned)
	}()

	capacity := rt.calculateCapacity(backendCount, numTrackers, ac)

	// Log capacity changes, especially when going to/from zero
	oldCapacity := rt.breaker.Capacity()

	// TODO: Remove this diagnostic log after capacity lag investigation is complete
	// Log all capacity updates to diagnose why capacity doesn't match pod count
	if capacity != int(oldCapacity) {
		rt.logger.Debugw("Revision capacity changing",
			"old-capacity", oldCapacity,
			"new-capacity", capacity,
			"backends", backendCount,
			"assigned-trackers", numTrackers,
			"total-pods-in-map", func() int {
				rt.mux.RLock()
				defer rt.mux.RUnlock()
				return len(rt.podTrackers)
			}(),
			"activator-index", ai,
			"activator-count", ac,
			"container-concurrency", rt.containerConcurrency.Load())
	}

	if capacity == 0 && oldCapacity > 0 {
		// Capacity dropped to zero - explain why
		rt.mux.RLock()
		totalPods := len(rt.podTrackers)
		rt.mux.RUnlock()

		rt.logger.Warnw("Revision capacity dropped to zero",
			"old-capacity", oldCapacity,
			"backends", backendCount,
			"assigned-trackers", numTrackers,
			"total-pods", totalPods,
			"activator-index", ai,
			"activator-count", ac)
	} else if capacity > 0 && oldCapacity == 0 {
		// Capacity increased from zero - waiting requests will now be unblocked
		rt.logger.Infow("Revision capacity restored (unblocking waiting requests)",
			"new-capacity", capacity,
			"backends", backendCount,
			"assigned-trackers", numTrackers,
			"activator-index", ai,
			"activator-count", ac)
	} else if capacity == 0 {
		// Starting with zero capacity - log reason
		rt.mux.RLock()
		totalPods := len(rt.podTrackers)
		rt.mux.RUnlock()

		if totalPods > 0 && numTrackers == 0 {
			rt.logger.Infow("Revision has zero capacity: no pods assigned to this activator",
				"total-pods", totalPods,
				"activator-index", ai,
				"activator-count", ac)
		} else if backendCount == 0 {
			rt.logger.Infow("Revision has zero capacity: no backends available",
				"backends", backendCount)
		}
	}

	rt.logger.Debugf("Set capacity to %d (backends: %d, index: %d/%d)",
		capacity, backendCount, ai, ac)

	// TODO: Remove this diagnostic log after capacity lag investigation is complete
	// When there's a significant gap between expected and actual capacity, log pod states
	expectedCapacity := int(rt.containerConcurrency.Load()) * numTrackers
	if expectedCapacity > 0 && capacity < expectedCapacity {
		rt.mux.RLock()
		podStates := make(map[string]podState)
		for dest, tracker := range rt.podTrackers {
			if tracker != nil {
				podStates[dest] = podState(tracker.state.Load())
			}
		}
		rt.mux.RUnlock()
		rt.logger.Warnw("Capacity gap detected",
			"expected-capacity", expectedCapacity,
			"actual-capacity", capacity,
			"capacity-gap", expectedCapacity-capacity,
			"backends-param", backendCount,
			"num-trackers", numTrackers,
			"pod-states", podStates)
	}

	rt.backendCount.Store(uint32(backendCount))
	rt.breaker.UpdateConcurrency(capacity)
}

func (rt *revisionThrottler) updateThrottlerState(backendCount int, newTrackers []*podTracker, healthyDests []string, drainingDests []string, clusterIPDest *podTracker) {
	defer func() {
		if r := recover(); r != nil {
			rt.logger.Errorf("Panic in revisionThrottler.updateThrottlerState: %v", r)
			panic(r)
		}
	}()

	rt.logger.Debugf("Updating Throttler %s: trackers = %d, backends = %d",
		rt.revID, len(newTrackers), backendCount)
	rt.logger.Debugf("Throttler %s DrainingDests: %s", rt.revID, drainingDests)
	rt.logger.Debugf("Throttler %s healthyDests: %s", rt.revID, healthyDests)

	// Update trackers / clusterIP before capacity. Otherwise we can race updating our breaker when
	// we increase capacity, causing a request to fall through before a tracker is added, causing an
	// incorrect LB decision.
	if func() bool {
		lockStart := time.Now()
		rt.mux.Lock()
		lockAcquireMs := float64(time.Since(lockStart).Milliseconds())
		if lockAcquireMs > 100 { // Log if lock acquisition took >100ms
			rt.logger.Warnw("Slow lock acquisition in updateThrottlerState",
				"lock-acquire-ms", lockAcquireMs)
		}

		lockHoldStart := time.Now()
		defer func() {
			lockHoldMs := float64(time.Since(lockHoldStart).Milliseconds())
			rt.mux.Unlock()
			if lockHoldMs > 100 { // Log if lock held >100ms
				rt.logger.Warnw("Lock held for long time in updateThrottlerState",
					"lock-hold-ms", lockHoldMs,
					"new-trackers", len(newTrackers),
					"healthy-dests", len(healthyDests),
					"draining-dests", len(drainingDests))
			}
		}()
		for _, t := range newTrackers {
			if t != nil {
				// Check if this dest was already in the map for a different tracker
				if existing, exists := rt.podTrackers[t.dest]; exists {
					// Validate the existing tracker's revision matches
					if existing.revisionID != rt.revID {
						rt.logger.Errorw("CRITICAL: Replacing tracker from WRONG REVISION - IP reuse across revisions!",
							"revision", rt.revID.String(),
							"dest", t.dest,
							"old-tracker-revision", existing.revisionID.String(),
							"old-tracker-id", existing.id,
							"old-created-at", existing.createdAt,
							"old-state", podState(existing.state.Load()),
							"new-tracker-id", t.id,
							"new-created-at", t.createdAt)
					} else {
						rt.logger.Warnw("Replacing existing pod tracker - possible IP reuse",
							"revision", rt.revID.String(),
							"dest", t.dest,
							"old-tracker-id", existing.id,
							"old-created-at", existing.createdAt,
							"old-state", podState(existing.state.Load()),
							"new-tracker-id", t.id,
							"new-created-at", t.createdAt)
					}
				}
				rt.podTrackers[t.dest] = t
				rt.logger.Debugw("Added pod tracker to revision map",
					"revision", rt.revID.String(),
					"dest", t.dest,
					"tracker-id", t.id,
					"total-trackers", len(rt.podTrackers))
			}
		}
		for _, d := range healthyDests {
			tracker := rt.podTrackers[d]
			if tracker != nil {
				currentState := podState(tracker.state.Load())

				// Check QP freshness to determine if we should trust informer
				lastQPSeen := tracker.lastQPUpdate.Load()
				qpAge := time.Now().Unix() - lastQPSeen
				lastQPEvent := ""
				if val := tracker.lastQPState.Load(); val != nil {
					if s, ok := val.(string); ok {
						lastQPEvent = s
					}
				}

				switch currentState {
				case podDraining, podRemoved:
					// Pod was being removed but is back in healthy endpoint list (e.g., rolling update rollback)
					// Reset to pending - QP will promote to healthy when ready
					tracker.state.Store(uint32(podPending))
					tracker.drainingStartTime.Store(0)
					rt.logger.Infow("Pod %s was draining/removed, now back in healthy list - resetting to pending", d)

				case podHealthy:
					// Already healthy, nothing to do

				case podPending:
					// K8s says healthy, pod is pending
					// Only promote if QP hasn't recently said "not-ready"
					if lastQPEvent == "not-ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
						rt.logger.Debugw("Ignoring K8s healthy - QP recently said not-ready",
							"dest", d,
							"qp-age-sec", qpAge,
							"qp-last-event", lastQPEvent)
						// Don't promote - trust fresh QP data
					} else {
						// Safe to promote (QP hasn't objected recently, or QP data is stale)
						if tracker.state.CompareAndSwap(uint32(podPending), uint32(podHealthy)) {
							rt.logger.Infow("K8s informer promoted pending pod to healthy",
								"dest", d,
								"qp-age-sec", qpAge)
						}
					}
				}
			}
		}
		// Handle pod draining to prevent dropped requests during pod removal
		now := time.Now().Unix()
		for _, d := range drainingDests {
			tracker := rt.podTrackers[d]
			if tracker == nil {
				continue
			}

			currentState := podState(tracker.state.Load())

			// Check QP freshness - if QP recently said "ready", K8s might be stale
			lastQPSeen := tracker.lastQPUpdate.Load()
			qpAge := now - lastQPSeen
			lastQPEvent := ""
			if val := tracker.lastQPState.Load(); val != nil {
				if s, ok := val.(string); ok {
					lastQPEvent = s
				}
			}

			// CRITICAL: If QP recently said "ready", don't drain (informer is stale)
			if currentState == podHealthy && lastQPEvent == "ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
				rt.logger.Warnw("Ignoring K8s draining signal - QP recently confirmed ready",
					"dest", d,
					"qp-age-sec", qpAge,
					"qp-last-event", lastQPEvent)
				continue
			}

			// If QP silent > 60s, trust informer (QP likely dead)
			if qpAge > int64(QPStalenessThreshold.Seconds()) || lastQPSeen == 0 {
				rt.logger.Debugw("QP silent - trusting K8s informer to drain pod",
					"dest", d,
					"qp-age-sec", qpAge)
			}

			switch currentState {
			case podHealthy:
				if tracker.tryDrain() {
					rt.logger.Debugf("Pod %s transitioning to draining state, refCount=%d", d, tracker.getRefCount())
					if tracker.getRefCount() == 0 {
						tracker.state.Store(uint32(podRemoved))
						delete(rt.podTrackers, d)
						rt.logger.Debugf("Pod %s removed immediately (no active requests)", d)
					}
				}
			case podDraining:
				refCount := tracker.getRefCount()
				if refCount == 0 {
					tracker.state.Store(uint32(podRemoved))
					delete(rt.podTrackers, d)
					rt.logger.Debugf("Pod %s removed after draining (no active requests)", d)
				} else {
					drainingStart := tracker.drainingStartTime.Load()
					if drainingStart > 0 && now-drainingStart > int64(maxDrainingDuration.Seconds()) {
						rt.logger.Warnf("Force removing pod %s stuck in draining state for %d seconds, refCount=%d", d, now-drainingStart, refCount)
						tracker.state.Store(uint32(podRemoved))
						delete(rt.podTrackers, d)
					}
				}
			case podPending:
				// Pod being removed while still pending
				tracker.state.Store(uint32(podRemoved))
				delete(rt.podTrackers, d)
				rt.logger.Debugf("Pod %s removed while pending", d)
			default:
				rt.logger.Errorf("Pod %s in unexpected state %d while processing draining destinations", d, tracker.state.Load())
			}
		}

		rt.clusterIPTracker = clusterIPDest
		return clusterIPDest != nil || len(rt.podTrackers) > 0
	}() {
		// If we have an address to target, then pass through an accurate
		// accounting of the number of backends.
		rt.updateCapacity(backendCount)
	} else {
		// If we do not have an address to target, then we should treat it
		// as though we have zero backends.
		rt.updateCapacity(0)
	}
}

// assignSlice picks a subset of the individual pods to send requests to
// for this Activator instance. This only matters in case of direct
// to pod IP routing, and is irrelevant, when ClusterIP is used.
// Uses consistent hashing to ensure all activators independently assign the correct endpoints.
func assignSlice(trackers map[string]*podTracker, selfIndex, numActivators int) []*podTracker {
	// Handle edge cases
	if selfIndex == -1 {
		// Sort for consistent ordering
		dests := maps.Keys(trackers)
		sort.Strings(dests)
		result := make([]*podTracker, 0, len(dests))
		for _, d := range dests {
			result = append(result, trackers[d])
		}
		return result
	}

	// Get sorted list of pod addresses for consistent ordering
	dests := maps.Keys(trackers)
	sort.Strings(dests)

	// If there's only one activator, it should handle all traffic regardless of its index
	// This handles edge cases where an activator might have a non-zero index but be the only one left
	if numActivators == 1 {
		assigned := make([]*podTracker, len(dests))
		for i, dest := range dests {
			assigned[i] = trackers[dest]
		}
		return assigned
	}

	// Use consistent hashing: take all pods where podIdx % numActivators == selfIndex
	assigned := make([]*podTracker, 0)
	for i, dest := range dests {
		if i%numActivators == selfIndex {
			assigned = append(assigned, trackers[dest])
		}
	}

	return assigned
}

// This function will never be called in parallel but `try` can be called in parallel to this so we need
// to lock on updating concurrency / trackers
func (rt *revisionThrottler) handleUpdate(update revisionDestsUpdate) {
	receiveTime := time.Now()

	// CRITICAL: Validate that this update is for the correct revision
	if update.Rev != rt.revID {
		rt.logger.Errorw("CRITICAL BUG: Received update for wrong revision - possible cross-revision contamination!",
			"expected-revision", rt.revID.String(),
			"received-revision", update.Rev.String(),
			"dests-count", len(update.Dests))
		// Do NOT process this update - it belongs to a different revision
		return
	}

	rt.logger.Debugw("Throttler received update from revision backends",
		"revision", rt.revID.String(),
		"dests-count", len(update.Dests),
		"cluster-ip", update.ClusterIPDest,
		"receive-time", receiveTime.Format(time.RFC3339Nano))

	// Force pod routing only - ignore ClusterIP routing entirely
	// ClusterIP is not yet ready, so we want to send requests directly to the pods.
	// NB: this will not be called in parallel, thus we can build a new podTrackers
	// map before taking out a lock.
	if true { // Always use pod routing, ignore update.ClusterIPDest
		// Loop over dests, reuse existing tracker if we have one, otherwise create
		// a new one.
		newTrackers := make([]*podTracker, 0, len(update.Dests))

		// Take read lock to safely access podTrackers map
		rt.mux.RLock()
		currentDests := maps.Keys(rt.podTrackers)
		newDestsSet := make(map[string]struct{}, len(update.Dests))
		for newDest := range update.Dests {
			newDestsSet[newDest] = struct{}{}
			tracker, ok := rt.podTrackers[newDest]
			if !ok {
				cc := int(rt.containerConcurrency.Load())
				if cc == 0 {
					tracker = newPodTracker(newDest, rt.revID, nil)
				} else {
					tracker = newPodTracker(newDest, rt.revID, queue.NewBreaker(queue.BreakerParams{
						QueueDepth:      breakerQueueDepth,
						MaxConcurrency:  cc,
						InitialCapacity: cc, // Presume full unused capacity.
					}))
				}
				rt.logger.Debugw("Creating new pod tracker",
					"revision", rt.revID.String(),
					"dest", newDest,
					"tracker-id", tracker.id,
					"container-concurrency", cc)
				newTrackers = append(newTrackers, tracker)
			} else {
				// Check current state and handle appropriately
				currentState := podState(tracker.state.Load())

				switch currentState {
				case podPending:
					// Pod is pending - let QP or informer promote it
					rt.logger.Debugf("Pod %s in pending state, waiting for promotion", newDest)
				case podDraining, podRemoved:
					// Pod was removed but is now back in the endpoint list (e.g., controller restart)
					// Reset to pending - QP will promote to healthy when ready
					tracker.state.Store(uint32(podPending))
					tracker.drainingStartTime.Store(0)
					rt.logger.Debugw("Re-adding previously draining/removed pod as pending",
						"dest", newDest,
						"previousState", currentState)
				case podHealthy:
					// Already healthy, nothing to do
				}
			}
		}
		healthyDests := make([]string, 0, len(currentDests))
		drainingDests := make([]string, 0, len(currentDests))
		for _, d := range currentDests {
			_, ok := newDestsSet[d]
			// If dest is no longer in the active set, it needs draining/removal
			if !ok {
				drainingDests = append(drainingDests, d)
			} else {
				healthyDests = append(healthyDests, d)
			}
		}
		rt.mux.RUnlock()

		// TODO: Remove this diagnostic log after capacity lag investigation is complete
		// Log to trace the update flow and timing
		rt.logger.Debugw("Calling updateThrottlerState from handleUpdate",
			"backend-count-param", len(update.Dests),
			"new-trackers-count", len(newTrackers),
			"healthy-dests-count", len(healthyDests),
			"draining-dests-count", len(drainingDests),
			"current-capacity", rt.breaker.Capacity())

		rt.updateThrottlerState(len(update.Dests), newTrackers, healthyDests, drainingDests, nil /*clusterIP*/)
		return
	}
	clusterIPPodTracker := newPodTracker(update.ClusterIPDest, rt.revID, nil)
	rt.updateThrottlerState(len(update.Dests), nil /*trackers*/, nil, nil, clusterIPPodTracker)
}

// addPodIncremental adds a single pod to the revision without removing other pods.
// This is used for push-based pod discovery where queue-proxies self-register.
// Unlike handleUpdate which is authoritative, this function only adds pods and never removes them.
// Handles 4 event types from queue-proxy:
// - "startup": creates podPending tracker (not yet viable for traffic)
// - "ready": promotes podPending → podHealthy (now viable for traffic)
// - "not-ready": demotes podHealthy → podPending (stops new traffic, preserves active requests)
// - "draining": transitions podHealthy → podDraining (graceful shutdown)
// QP events are AUTHORITATIVE and override K8s informer state (see QP_AUTHORITATIVE_REFACTOR.md)
func (rt *revisionThrottler) addPodIncremental(podIP string, eventType string, logger *zap.SugaredLogger) {
	rt.mux.Lock()

	existing, exists := rt.podTrackers[podIP]

	if exists {
		// Update QP freshness tracking
		existing.lastQPUpdate.Store(time.Now().Unix())
		existing.lastQPState.Store(eventType)

		currentState := podState(existing.state.Load())

		switch eventType {
		case "startup":
			// Duplicate startup - noop
			logger.Debugw("Duplicate startup event (pod already tracked)",
				"revision", rt.revID.String(),
				"pod-ip", podIP,
				"current-state", currentState)

		case "ready":
			// QP says ready - promote to healthy immediately
			if currentState == podPending {
				if existing.state.CompareAndSwap(uint32(podPending), uint32(podHealthy)) {
					logger.Infow("QP promoted pending pod to healthy",
						"revision", rt.revID.String(),
						"pod-ip", podIP)
				}
			} else {
				logger.Debugw("Duplicate ready event (pod already in non-pending state)",
					"revision", rt.revID.String(),
					"pod-ip", podIP,
					"current-state", currentState)
			}

		case "not-ready":
			// QP says not-ready - demote to pending
			// CRITICAL: Preserves breaker and refCount - just stops NEW traffic
			if currentState == podHealthy {
				if existing.state.CompareAndSwap(uint32(podHealthy), uint32(podPending)) {
					logger.Warnw("QP demoted pod to pending (readiness probe failed)",
						"revision", rt.revID.String(),
						"pod-ip", podIP,
						"active-requests", existing.refCount.Load())
				}
			} else {
				logger.Debugw("not-ready event on non-healthy pod",
					"revision", rt.revID.String(),
					"pod-ip", podIP,
					"current-state", currentState)
			}

		case "draining":
			// QP says draining - transition to draining state
			if existing.tryDrain() {
				logger.Infow("QP initiated pod draining",
					"revision", rt.revID.String(),
					"pod-ip", podIP,
					"active-requests", existing.refCount.Load())
			} else {
				logger.Debugw("draining event on non-healthy pod (already draining/removed)",
					"revision", rt.revID.String(),
					"pod-ip", podIP,
					"current-state", currentState)
			}
		}

		rt.mux.Unlock()
		return
	}

	// Pod doesn't exist - create new tracker
	cc := int(rt.containerConcurrency.Load())
	var tracker *podTracker
	if cc == 0 {
		tracker = newPodTracker(podIP, rt.revID, nil)
	} else {
		tracker = newPodTracker(podIP, rt.revID, queue.NewBreaker(queue.BreakerParams{
			QueueDepth:      breakerQueueDepth,
			MaxConcurrency:  cc,
			InitialCapacity: cc,
		}))
	}

	// Set initial state based on event type
	// For "ready" events, assume pod is healthy; for others, mark as pending
	if eventType == "ready" {
		tracker.state.Store(uint32(podHealthy))
	} else {
		tracker.state.Store(uint32(podPending))
	}

	// Initialize QP tracking for new pod
	tracker.lastQPUpdate.Store(time.Now().Unix())
	tracker.lastQPState.Store(eventType)

	// Add tracker to the map
	rt.podTrackers[podIP] = tracker
	podCount := len(rt.podTrackers)
	logger.Infow("Discovered new pod via push-based registration",
		"revision", rt.revID.String(),
		"pod-ip", podIP,
		"event-type", eventType,
		"initial-state", podState(tracker.state.Load()),
		"total-trackers-now", podCount)

	rt.mux.Unlock()

	// CRITICAL: Must call updateCapacity OUTSIDE the lock to avoid deadlock.
	// updateCapacity needs to acquire rt.mux.RLock() internally, which is incompatible
	// with holding rt.mux.Lock() from above.
	rt.updateCapacity(podCount)
}

// Throttler load balances requests to revisions based on capacity. When `Run` is called it listens for
// updates to revision backends and decides when and when and where to forward a request.
type Throttler struct {
	revisionThrottlers      map[types.NamespacedName]*revisionThrottler
	revisionThrottlersMutex sync.RWMutex
	revisionLister          servinglisters.RevisionLister
	ipAddress               string // The IP address of this activator.
	logger                  *zap.SugaredLogger
	epsUpdateCh             chan *corev1.Endpoints
}

// NewThrottler creates a new Throttler
func NewThrottler(ctx context.Context, ipAddr string) *Throttler {
	revisionInformer := revisioninformer.Get(ctx)
	t := &Throttler{
		revisionThrottlers: make(map[types.NamespacedName]*revisionThrottler),
		revisionLister:     revisionInformer.Lister(),
		ipAddress:          ipAddr,
		logger:             logging.FromContext(ctx),
		epsUpdateCh:        make(chan *corev1.Endpoints),
	}

	// Watch revisions to create throttler with backlog immediately and delete
	// throttlers on revision delete
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.revisionUpdated,
		UpdateFunc: controller.PassNew(t.revisionUpdated),
		DeleteFunc: t.revisionDeleted,
	})

	// Watch activator endpoint to maintain activator count
	endpointsInformer := endpointsinformer.Get(ctx)

	// Handles public service updates.
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.LabelFilterFunc(networking.ServiceTypeKey,
			string(networking.ServiceTypePublic), false),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    t.publicEndpointsUpdated,
			UpdateFunc: controller.PassNew(t.publicEndpointsUpdated),
		},
	})
	return t
}

// Run starts the throttler and blocks until the context is done.
func (t *Throttler) Run(ctx context.Context, probeTransport http.RoundTripper, usePassthroughLb bool, meshMode netcfg.MeshCompatibilityMode) {
	rbm := newRevisionBackendsManager(ctx, probeTransport, usePassthroughLb, meshMode)
	// Update channel is closed when ctx is done.
	t.run(rbm.updates())
}

func (t *Throttler) run(updateCh <-chan revisionDestsUpdate) {
	for {
		select {
		case update, ok := <-updateCh:
			if !ok {
				t.logger.Info("The Throttler has stopped.")
				return
			}
			t.handleUpdate(update)
		case eps := <-t.epsUpdateCh:
			t.handlePubEpsUpdate(eps)
		}
	}
}

// Try waits for capacity and then executes function, passing in a l4 dest to send a request
func (t *Throttler) Try(ctx context.Context, revID types.NamespacedName, xRequestId string, function func(string, bool) error) error {
	// Log at entry point to track revision assignment
	t.logger.Debugw("Request entering throttler",
		"x-request-id", xRequestId,
		"revision", revID.String())

	rt, err := t.getOrCreateRevisionThrottler(revID)
	if err != nil {
		return err
	}

	// Verify we got the correct throttler
	if rt.revID != revID {
		t.logger.Errorw("CRITICAL BUG: getOrCreateRevisionThrottler returned wrong throttler!",
			"requested-revision", revID.String(),
			"returned-revision", rt.revID.String(),
			"x-request-id", xRequestId)
		return fmt.Errorf("throttler mismatch: requested %s but got %s", revID.String(), rt.revID.String())
	}

	return rt.try(ctx, xRequestId, function)
}

// HandlePodRegistration processes a push-based pod registration from a queue-proxy.
// This allows immediate pod discovery without waiting for K8s informer updates (which can take 60-70 seconds).
// eventType should be "startup" (creates podPending tracker) or "ready" (promotes to podHealthy).
// This uses a dedicated incremental add function instead of the authoritative handleUpdate flow,
// so push-based registrations never remove existing pods.
func (t *Throttler) HandlePodRegistration(revID types.NamespacedName, podIP string, eventType string, logger *zap.SugaredLogger) {
	if podIP == "" {
		logger.Debugw("Ignoring pod registration with empty pod IP",
			"revision", revID.String(),
			"event-type", eventType)
		return
	}

	logger.Debugw("Throttler received pod registration",
		"revision", revID.String(),
		"pod-ip", podIP,
		"event-type", eventType)

	rt, err := t.getOrCreateRevisionThrottler(revID)
	if err != nil {
		logger.Errorw("Failed to get revision throttler for pod registration",
			"revision", revID.String(),
			"pod-ip", podIP,
			"error", err)
		return
	}

	// Call the dedicated incremental add function instead of handleUpdate
	// This ensures push-based registrations only add pods, never remove them
	rt.addPodIncremental(podIP, eventType, logger)
}

func (t *Throttler) getOrCreateRevisionThrottler(revID types.NamespacedName) (*revisionThrottler, error) {
	// First, see if we can succeed with just an RLock. This is in the request path so optimizing
	// for this case is important
	t.revisionThrottlersMutex.RLock()
	revThrottler, ok := t.revisionThrottlers[revID]
	t.revisionThrottlersMutex.RUnlock()
	if ok {
		return revThrottler, nil
	}

	// Redo with a write lock since we failed the first time and may need to create
	t.revisionThrottlersMutex.Lock()
	defer t.revisionThrottlersMutex.Unlock()
	revThrottler, ok = t.revisionThrottlers[revID]
	if !ok {
		rev, err := t.revisionLister.Revisions(revID.Namespace).Get(revID.Name)
		if err != nil {
			return nil, err
		}
		revThrottler = newRevisionThrottler(
			revID,
			rev.Spec.LoadBalancingPolicy,
			int(rev.Spec.GetContainerConcurrency()),
			pkgnet.ServicePortName(rev.GetProtocol()),
			queue.BreakerParams{QueueDepth: breakerQueueDepth, MaxConcurrency: revisionMaxConcurrency},
			t.logger,
		)
		t.revisionThrottlers[revID] = revThrottler
	}
	return revThrottler, nil
}

// revisionUpdated is used to ensure we have a backlog set up for a revision as soon as it is created
// rather than erroring with revision not found until a networking probe succeeds
func (t *Throttler) revisionUpdated(obj any) {
	rev := obj.(*v1.Revision)
	revID := types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}

	t.logger.Debug("Revision update", zap.String(logkey.Key, revID.String()))

	if rt, err := t.getOrCreateRevisionThrottler(revID); err != nil {
		t.logger.Errorw("Failed to get revision throttler for revision",
			zap.Error(err), zap.String(logkey.Key, revID.String()))
	} else if rt != nil {
		// Update the lbPolicy dynamically if the revision's spec policy changed
		newPolicy, name := pickLBPolicy(rev.Spec.LoadBalancingPolicy, nil, int(rev.Spec.GetContainerConcurrency()), t.logger)
		// Use atomic store for lock-free access in the hot request path
		rt.lbPolicy.Store(newPolicy)
		rt.containerConcurrency.Store(uint32(rev.Spec.GetContainerConcurrency()))
		t.logger.Debugf("Updated revision throttler LB policy to: %s", name)
	}
}

// revisionDeleted is to clean up revision throttlers after a revision is deleted to prevent unbounded
// memory growth
func (t *Throttler) revisionDeleted(obj any) {
	acc, err := kmeta.DeletionHandlingAccessor(obj)
	if err != nil {
		t.logger.Warnw("Revision delete failure to process", zap.Error(err))
		return
	}

	revID := types.NamespacedName{Namespace: acc.GetNamespace(), Name: acc.GetName()}

	t.logger.Debugw("Revision delete", zap.String(logkey.Key, revID.String()))

	t.revisionThrottlersMutex.Lock()
	defer t.revisionThrottlersMutex.Unlock()
	delete(t.revisionThrottlers, revID)
}

func (t *Throttler) handleUpdate(update revisionDestsUpdate) {
	if rt, err := t.getOrCreateRevisionThrottler(update.Rev); err != nil {
		if k8serrors.IsNotFound(err) {
			t.logger.Debugw("Revision not found. It was probably removed", zap.String(logkey.Key, update.Rev.String()))
		} else {
			t.logger.Errorw("Failed to get revision throttler", zap.Error(err), zap.String(logkey.Key, update.Rev.String()))
		}
	} else {
		rt.handleUpdate(update)
	}
}

func (t *Throttler) handlePubEpsUpdate(eps *corev1.Endpoints) {
	t.logger.Debugf("Public EPS updates: %#v", eps)

	revN := eps.Labels[serving.RevisionLabelKey]
	if revN == "" {
		// Perhaps, we're not the only ones using the same selector label.
		t.logger.Warnf("Ignoring update for PublicService %s/%s", eps.Namespace, eps.Name)
		return
	}
	rev := types.NamespacedName{Name: revN, Namespace: eps.Namespace}
	if rt, err := t.getOrCreateRevisionThrottler(rev); err != nil {
		if k8serrors.IsNotFound(err) {
			t.logger.Debugw("Revision not found. It was probably removed", zap.String(logkey.Key, rev.String()))
		} else {
			t.logger.Errorw("Failed to get revision throttler", zap.Error(err), zap.String(logkey.Key, rev.String()))
		}
	} else {
		rt.handlePubEpsUpdate(eps, t.ipAddress)
	}
}

func (rt *revisionThrottler) handlePubEpsUpdate(eps *corev1.Endpoints, selfIP string) {
	// NB: this is guaranteed to be executed on a single thread.
	epSet := healthyAddresses(eps, rt.protocol)
	if !epSet.Has(selfIP) {
		// No need to do anything, this activator is not in path.
		return
	}

	// We are using List to have the IP addresses sorted for consistent results.
	epsL := sets.List(epSet)
	//nolint:gosec // number of k8s replicas is bounded by int32
	newNA, newAI := int32(len(epsL)), int32(inferIndex(epsL, selfIP))
	if newAI == -1 {
		// No need to do anything, this activator is not in path.
		return
	}

	na, ai := rt.numActivators.Load(), rt.activatorIndex.Load()
	if na == uint32(newNA) && ai == newAI {
		// The state didn't change, do nothing
		return
	}

	rt.numActivators.Store(uint32(newNA))
	rt.activatorIndex.Store(newAI)
	rt.logger.Debugf("This activator index is %d/%d was %d/%d",
		newAI, newNA, ai, na)

	// TODO: Remove this diagnostic log after capacity lag investigation is complete
	// Log activator-triggered capacity updates to see if they're overwriting pod updates
	storedBackendCount := int(rt.backendCount.Load())
	rt.mux.RLock()
	actualPodCount := len(rt.podTrackers)
	rt.mux.RUnlock()
	if storedBackendCount != actualPodCount {
		rt.logger.Warnw("Activator update triggering capacity recalc with stale backend count",
			"stored-backend-count", storedBackendCount,
			"actual-pods-in-map", actualPodCount,
			"new-activator-index", newAI,
			"new-activator-count", newNA,
			"old-activator-index", ai,
			"old-activator-count", na)
	}

	rt.updateCapacity(storedBackendCount)
}

// inferIndex returns the index of this activator slice.
// If inferIndex returns -1, it means that this activator will not receive
// any traffic just yet so, do not participate in slicing, this happens after
// startup, but before this activator is threaded into the endpoints
// (which is up to 10s after reporting healthy).
// For now we are just sorting the IP addresses of all activators
// and finding our index in that list.
func inferIndex(eps []string, ipAddress string) int {
	idx := sort.SearchStrings(eps, ipAddress)

	// Check if this activator is part of the endpoints slice?
	if idx == len(eps) || eps[idx] != ipAddress {
		return -1
	}
	return idx
}

func (t *Throttler) publicEndpointsUpdated(newObj any) {
	endpoints := newObj.(*corev1.Endpoints)
	t.logger.Debug("Updated public Endpoints: ", endpoints.Name)
	t.epsUpdateCh <- endpoints
}

// minOneOrValue function returns num if its greater than 1
// else the function returns 1
func minOneOrValue(num int) int {
	if num > 1 {
		return num
	}
	return 1
}

// infiniteBreaker is basically a short circuit.
// infiniteBreaker provides us capability to send unlimited number
// of requests to the downstream system.
// This is to be used only when the container concurrency is unset
// (i.e. infinity).
// The infiniteBreaker will, though, block the requests when
// downstream capacity is 0.
type infiniteBreaker struct {
	// mu guards `broadcast` channel.
	mu sync.RWMutex

	// broadcast channel is used notify the waiting requests that
	// downstream capacity showed up.
	// When the downstream capacity switches from 0 to 1, the channel is closed.
	// When the downstream capacity disappears, the a new channel is created.
	// Reads/Writes to the `broadcast` must be guarded by `mu`.
	broadcast chan struct{}

	// concurrency in the infinite breaker takes only two values
	// 0 (no downstream capacity) and 1 (infinite downstream capacity).
	// `Maybe` checks this value to determine whether to proxy the request
	// immediately or wait for capacity to appear.
	concurrency atomic.Uint32

	logger *zap.SugaredLogger
}

// newInfiniteBreaker creates an infiniteBreaker
func newInfiniteBreaker(logger *zap.SugaredLogger) *infiniteBreaker {
	return &infiniteBreaker{
		broadcast: make(chan struct{}),
		logger:    logger,
	}
}

// Capacity returns the current capacity of the breaker
func (ib *infiniteBreaker) Capacity() uint64 {
	return uint64(ib.concurrency.Load()) //nolint:gosec // concurrency is always 0 or 1
}

// Pending returns the current pending requests the breaker
func (ib *infiniteBreaker) Pending() int {
	return int(ib.concurrency.Load())
}

// Pending returns the current inflight requests the breaker
func (ib *infiniteBreaker) InFlight() uint64 {
	return uint64(ib.concurrency.Load()) //nolint:gosec // concurrency is always 0 or 1
}

func zeroOrOne(x int) uint32 {
	if x == 0 {
		return 0
	}
	return 1
}

// UpdateConcurrency sets the concurrency of the breaker
func (ib *infiniteBreaker) UpdateConcurrency(cc int) {
	rcc := zeroOrOne(cc)
	// We lock here to make sure two scale up events don't
	// stomp on each other's feet.
	ib.mu.Lock()
	defer ib.mu.Unlock()
	old := ib.concurrency.Swap(rcc)

	// Scale up/down event.
	if old != rcc {
		if rcc == 0 {
			// Scaled to 0.
			ib.broadcast = make(chan struct{})
		} else {
			close(ib.broadcast)
		}
	}
}

// Maybe executes thunk when capacity is available
func (ib *infiniteBreaker) Maybe(ctx context.Context, thunk func()) error {
	has := ib.Capacity()
	// We're scaled to serve.
	if has > 0 {
		thunk()
		return nil
	}

	// Make sure we lock to get the channel, to avoid
	// race between Maybe and UpdateConcurrency.
	var ch chan struct{}
	ib.mu.RLock()
	ch = ib.broadcast
	ib.mu.RUnlock()
	select {
	case <-ch:
		// Scaled up.
		thunk()
		return nil
	case <-ctx.Done():
		ib.logger.Info("Context is closed: ", ctx.Err())
		return ctx.Err()
	}
}

func (ib *infiniteBreaker) Reserve(context.Context) (func(), bool) { return noop, true }
