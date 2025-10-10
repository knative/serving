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
	"net"
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
	netheader "knative.dev/networking/pkg/http/header"
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
)

func newPodTracker(dest string, b breaker) *podTracker {
	tracker := &podTracker{
		id:        string(uuid.NewUUID()),
		createdAt: time.Now().UnixMicro(),
		dest:      dest,
		b:         b,
	}
	tracker.state.Store(uint32(podPending)) // Start in pending state until health verified
	tracker.refCount.Store(0)
	tracker.drainingStartTime.Store(0)
	tracker.weight.Store(0)
	tracker.decreaseWeight = func() {
		if tracker.weight.Load() > 0 {
			tracker.weight.Add(^uint32(0))
		}
	} // Subtract 1 with underflow protection

	return tracker
}

type podState uint32

const (
	podHealthy podState = iota
	podDraining
	podQuarantined
	podRecovering
	podRemoved
	podPending // New state for pods that haven't been health-checked yet
)

type podTracker struct {
	id        string
	createdAt int64
	dest      string
	b         breaker

	// State machine for pod health transitions
	state atomic.Uint32 // Uses podState constants
	// Reference count for in-flight requests to support graceful draining
	refCount atomic.Uint64
	// Unix timestamp when the pod entered draining state
	drainingStartTime atomic.Int64
	// Unix timestamp when the pod should exit quarantine state
	quarantineEndTime atomic.Int64
	// Number of consecutive quarantine events for this pod
	quarantineCount atomic.Uint32

	// TODO: Should we clarify what makes a pod unhealthy (failed health checks, etc.)?

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
	if p.state.CompareAndSwap(uint32(podRecovering), uint32(podDraining)) {
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
	// Only healthy and pending pods can be reserved
	// Pending pods will be health-checked later in the request flow
	if state != podHealthy && state != podPending && state != podRecovering {
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

// transitionOutOfQuarantine ensures we decrement quarantine gauge exactly once
// and optionally set a new state. Returns true if a decrement happened.
func transitionOutOfQuarantine(ctx context.Context, p *podTracker, newState podState) bool {
	if p == nil {
		return false
	}
	// Use CAS to atomically transition from quarantined to new state
	if p.state.CompareAndSwap(uint32(podQuarantined), uint32(newState)) {
		// Successfully transitioned out of quarantine
		handler.RecordPodQuarantineChange(ctx, -1)
		handler.RecordPodQuarantineExit(ctx) // New counter-based metric
		return true
	}

	// Try to update to new state if it's different from current
	// This handles the case where we're not in quarantine but need state update
	prev := podState(p.state.Load())
	if newState != prev && prev != podQuarantined {
		p.state.CompareAndSwap(uint32(prev), uint32(newState))
	}
	return false
}

// filterAvailableTrackers filters out quarantined pods and recovers expired quarantines
func (rt *revisionThrottler) filterAvailableTrackers(ctx context.Context, trackers []*podTracker) []*podTracker {
	available := make([]*podTracker, 0, len(trackers))
	now := time.Now().Unix()

	for _, tracker := range trackers {
		if tracker == nil {
			continue
		}

		state := podState(tracker.state.Load())

		// Check if quarantined and if quarantine period has expired
		if state == podQuarantined {
			if now >= tracker.quarantineEndTime.Load() {
				// Quarantine expired, move to recovering state for health verification
				transitionOutOfQuarantine(ctx, tracker, podRecovering)
				rt.logger.Infof("Pod %s quarantine expired, entering recovery state", tracker.dest)
				available = append(available, tracker)
			}
			// Skip quarantined pods that haven't expired
			continue
		}

		// Include healthy, recovering, and pending pods
		// Pending pods will be health-checked on first use
		if state == podHealthy || state == podRecovering || state == podPending {
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

	// Filter out quarantined pods and recover expired quarantines
	originalCount := len(rt.assignedTrackers)
	availableTrackers := rt.filterAvailableTrackers(ctx, rt.assignedTrackers)

	// Log availability issues
	if len(availableTrackers) == 0 && originalCount > 0 {
		rt.logger.Warnw("No available pods after filtering",
			"assigned", originalCount,
			"available", 0,
			"revision", rt.revID.String())
	} else if originalCount > 2 && len(availableTrackers) < originalCount/2 {
		rt.logger.Warnw("Many pods unavailable",
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
				rt.logger.Warnf("Request %s exceeded ERROR proxy queue time threshold (60s)", xRequestId)
			case <-criticalTimer.C:
				handler.RecordProxyQueueTimeCritical(ctx)
				rt.logger.Warnf("Request %s exceeded CRITICAL proxy queue time threshold (3m)", xRequestId)
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
			rt.logger.Infow("Request retry attempt",
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

		// Check if we're about to wait for capacity
		var waitStartTime time.Time
		if rt.breaker.Pending() > 0 {
			waitStartTime = time.Now()
			rt.logger.Infow("Request waiting for breaker capacity",
				"x-request-id", xRequestId,
				"pending", rt.breaker.Pending(),
				"capacity", rt.breaker.Capacity(),
				"in-flight", rt.breaker.InFlight())
		}

		if err := rt.breaker.Maybe(ctx, func() {
			if !waitStartTime.IsZero() {
				waitMs := float64(time.Since(waitStartTime).Milliseconds())
				if waitMs > 100 { // Log if waited more than 100ms
					rt.logger.Warnw("Request waited for breaker capacity",
						"x-request-id", xRequestId,
						"wait-ms", waitMs)
				}
			}
			callback, tracker, isClusterIP := rt.acquireDest(ctx)
			if tracker == nil {
				// Check if all pods are quarantined
				rt.mux.RLock()
				allQuarantined := true
				assignedCount := len(rt.assignedTrackers)
				quarantinedCount := 0
				for _, t := range rt.assignedTrackers {
					if t != nil {
						if podState(t.state.Load()) == podQuarantined {
							quarantinedCount++
						} else {
							allQuarantined = false
						}
					}
				}
				rt.mux.RUnlock()

				if allQuarantined && assignedCount > 0 {
					rt.logger.Warnw("all pods quarantined; re-enqueue",
						"x-request-id", xRequestId,
						"assigned", assignedCount,
						"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()),
					)
					reenqueue = true
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
				rt.logger.Infow("Failed to acquire tracker; re-enqueue",
					"x-request-id", xRequestId,
					"quarantined", quarantinedCount,
					"assigned", assignedCount,
					"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()))
				return
			}
			trackerId := tracker.id
			rt.logger.Debugf("%s -> Acquired Pod Tracker %s - %s (createdAt %d)", xRequestId, trackerId, tracker.dest, tracker.createdAt)
			rt.logger.Debugf("Tracker %s Breaker State: capacity: %d, inflight: %d, pending: %d", trackerId, tracker.Capacity(), tracker.InFlight(), tracker.Pending())
			defer func() {
				callback()
				rt.logger.Debugf("%s -> %s breaker release semaphore\n", xRequestId, trackerId)
			}()
			// We already reserved a guaranteed spot. So just execute the passed functor.
			// ClusterIP routing is intentionally disabled; we route directly to pods.
			// First, perform a pod ready check to ensure the queue-proxy is healthy
			// Only do this for pod routing (not clusterIP)
			if !isClusterIP {
				currentState := podState(tracker.state.Load())
				rt.logger.Debugw("pod ready check attempt", "x-request-id", xRequestId, "dest", tracker.dest, "state", currentState)

				if !tcpPingCheck(tracker.dest) {
					// For pending pods, this is their first health check
					if currentState == podPending {
						rt.logger.Infow("pod ready check failed for pending pod; quarantine",
							"x-request-id", xRequestId,
							"dest", tracker.dest)
					} else {
						rt.logger.Errorw("pod ready check failed; quarantine",
							"x-request-id", xRequestId,
							"dest", tracker.dest)
					}
					// Try to quarantine this tracker using CAS to avoid races
					// We can transition from any non-quarantined state to quarantined
					wasQuarantined := false
					for !wasQuarantined {
						prevState := podState(tracker.state.Load())
						if prevState == podQuarantined {
							// Already quarantined by another goroutine
							break
						}
						if tracker.state.CompareAndSwap(uint32(prevState), uint32(podQuarantined)) {
							wasQuarantined = true
							// Only update metrics if we actually performed the quarantine
							// Increment consecutive quarantine count
							count := tracker.quarantineCount.Add(1)
							// Determine backoff duration
							// Use aggressive backoff for pods that were pending (never been healthy)
							wasPending := currentState == podPending
							backoff := quarantineBackoffSeconds(count, wasPending)
							tracker.quarantineEndTime.Store(time.Now().Unix() + int64(backoff))
							// Record metrics
							handler.RecordPodQuarantineChange(ctx, 1)
							handler.RecordPodQuarantineEntry(ctx) // New counter-based metric
							handler.RecordTCPPingFailureEvent(ctx)
						}
					}
					// Re-queue the request to try another backend
					reenqueue = true
					return
				}

				// Pod ready check successful - if pod was pending, promote to healthy
				if currentState == podPending {
					// Use CAS to atomically transition from pending to healthy
					if tracker.state.CompareAndSwap(uint32(podPending), uint32(podHealthy)) {
						rt.logger.Infow("Pending pod passed ready check, promoting to healthy",
							"x-request-id", xRequestId,
							"dest", tracker.dest)
					}
				}
			}

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

			// Handle successful request completion
			if ret == nil { // Successful request
				currentState := podState(tracker.state.Load())
				// If pod is recovering, promote to healthy after successful request
				if currentState == podRecovering {
					// Use CAS to atomically transition from recovering to healthy
					if tracker.state.CompareAndSwap(uint32(podRecovering), uint32(podHealthy)) {
						tracker.quarantineCount.Store(0)
						rt.logger.Infof("Pod %s successfully recovered, promoting to healthy state", tracker.dest)
					}
				}
				// On success, if pod was previously quarantined, reset the counter to reduce future backoff
				// Re-check state after potential transition
				currentState = podState(tracker.state.Load())
				if currentState == podHealthy && tracker.quarantineCount.Load() > 0 {
					prevCount := tracker.quarantineCount.Swap(0)
					rt.logger.Infow("Pod quarantine history cleared after success",
						"x-request-id", xRequestId,
						"dest", tracker.dest,
						"previous-quarantine-count", prevCount)
				}
			}

			// Check if we got a quick 502 response
			// TODO: TEMPORARILY DISABLED - Re-enable quick 502 quarantine functionality
			// This feature was disabled due to issues with quick 502s causing unnecessary quarantines
			// var quick502Err handler.ErrQuick502
			// if errors.As(ret, &quick502Err) {
			// 	rt.logger.Errorw("instant 502; quarantine", "x-request-id", xRequestId, "dest", tracker.dest, "duration", quick502Err.Duration.String())
			//
			// 	// Quarantine this tracker
			// 	tracker.state.Store(uint32(podQuarantined))
			// 	// Increment consecutive quarantine count
			// 	count := tracker.quarantineCount.Add(1)
			// 	// Determine backoff duration
			// 	backoff := quarantineBackoffSeconds(count)
			// 	tracker.quarantineEndTime.Store(time.Now().Unix() + int64(backoff))
			//
			// 	// Record metrics
			//
			// 	handler.RecordPodQuarantineChange(ctx, 1)
			// 	handler.RecordPodQuarantineEntry(ctx) // New counter-based metric
			// 	handler.RecordImmediate502Event(ctx)
			//
			// 	// Re-queue the request to try another backend
			// 	reenqueue = true
			// 	ret = nil // Clear error so we retry
			// 	return
			// }
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
		rt.mux.RUnlock()

		rt.logger.Debugf("Trackers %d/%d: assignment: %v", ai, ac, assigned)

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
	rt.logger.Debugf("Set capacity to %d (backends: %d, index: %d/%d)",
		capacity, backendCount, ai, ac)

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
		rt.mux.Lock()
		defer rt.mux.Unlock()
		for _, t := range newTrackers {
			if t != nil {
				rt.podTrackers[t.dest] = t
			}
		}
		for _, d := range healthyDests {
			tracker := rt.podTrackers[d]
			if tracker != nil {
				currentState := podState(tracker.state.Load())
				// Handle pods that K8s reports as healthy but activator has in non-healthy states
				switch currentState {
				case podDraining, podRemoved:
					// Pod was being removed but is back in healthy endpoint list (e.g., rolling update rollback)
					// Reset to pending for re-validation rather than trusting immediately
					tracker.state.Store(uint32(podPending))
					tracker.drainingStartTime.Store(0)
					rt.logger.Infof("Pod %s was draining/removed, now back in healthy list - resetting to pending", d)
				case podHealthy:
					// Already healthy, nothing to do
				case podRecovering:
					// Let it recover naturally after successful request - don't interfere
				case podQuarantined:
					// Pod is in backoff - don't short-circuit quarantine just because K8s says it's ready
					// Let the quarantine timer complete naturally
				case podPending:
					// Already pending, will be health-checked on first request
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

			switch podState(tracker.state.Load()) {
			case podHealthy, podRecovering:
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
			case podQuarantined:
				// Pod being removed while quarantined - decrement gauge exactly once
				transitionOutOfQuarantine(context.Background(), tracker, podRemoved)
				delete(rt.podTrackers, d)
				rt.logger.Debugf("Pod %s removed while in quarantine", d)
			case podPending:
				// Pod being removed while still pending health check
				tracker.state.Store(uint32(podRemoved))
				delete(rt.podTrackers, d)
				rt.logger.Debugf("Pod %s removed while pending health check", d)
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
	rt.logger.Debugw("Handling update",
		zap.String("ClusterIP", update.ClusterIPDest), zap.Object("dests", logging.StringSet(update.Dests)))

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
					tracker = newPodTracker(newDest, nil)
				} else {
					tracker = newPodTracker(newDest, queue.NewBreaker(queue.BreakerParams{
						QueueDepth:      breakerQueueDepth,
						MaxConcurrency:  cc,
						InitialCapacity: cc, // Presume full unused capacity.
					}))
				}
				newTrackers = append(newTrackers, tracker)
			} else {
				// Check current state and handle appropriately
				currentState := podState(tracker.state.Load())

				// Don't disrupt pods that are recovering, quarantined, or pending
				// These states have their own recovery mechanisms with backoff timers
				// Resetting them to pending would restart the health check process and interfere with recovery
				switch currentState {
				case podRecovering, podQuarantined, podPending:
					// These states have their own recovery mechanisms - don't interfere
					rt.logger.Debugf("Pod %s in state %v, allowing natural recovery", newDest, currentState)
				case podDraining, podRemoved:
					// Pod was removed but is now back in the endpoint list (e.g., controller restart)
					// Reset to pending for re-validation
					tracker.state.Store(uint32(podPending))
					tracker.drainingStartTime.Store(0)
					rt.logger.Infow("Re-adding previously draining/removed pod as pending",
						"dest", newDest,
						"previousState", currentState) // Log BEFORE changing state
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

		rt.updateThrottlerState(len(update.Dests), newTrackers, healthyDests, drainingDests, nil /*clusterIP*/)
		return
	}
	clusterIPPodTracker := newPodTracker(update.ClusterIPDest, nil)
	rt.updateThrottlerState(len(update.Dests), nil /*trackers*/, nil, nil, clusterIPPodTracker)
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
	rt, err := t.getOrCreateRevisionThrottler(revID)
	if err != nil {
		return err
	}
	return rt.try(ctx, xRequestId, function)
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
	rt.updateCapacity(int(rt.backendCount.Load()))
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

// podReadyCheckFunc is a function variable that can be overridden for testing
// Use atomic.Value for thread-safe access
var podReadyCheckFunc atomic.Value

func init() {
	podReadyCheckFunc.Store(podReadyCheck)
}

// podReadyCheck performs an HTTP health check to verify the queue-proxy is ready
// Returns true if the health endpoint returns 200 OK, false otherwise (503 = draining)
// Using podReadyCheckTimeout (500ms) to reduce latency during pod health checks
func podReadyCheck(dest string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), podReadyCheckTimeout)
	defer cancel()

	// Create HTTP request to queue-proxy health endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+dest+"/", nil)
	if err != nil {
		return false
	}

	// Set User-Agent so Drainer recognizes this as a health check (kube-probe prefix)
	// This allows the Drainer to intercept and return 503 when draining
	req.Header.Set("User-Agent", "kube-probe/activator")

	// Add Knative probe header so queue-proxy health handler processes the request
	req.Header.Set(netheader.ProbeKey, queue.Name)

	// Use a custom client with short timeout
	client := &http.Client{
		Timeout: podReadyCheckTimeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: (&net.Dialer{
				Timeout: podReadyCheckTimeout,
			}).DialContext,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Consider any 2XX response as healthy
	// 503 Service Unavailable means the queue-proxy is draining
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// quarantineBackoffSeconds returns backoff seconds for a given consecutive quarantine count.
// For pending pods (never been healthy): 0s, 1s, 1s, 2s, 5s (be aggressive in retrying new pods)
// For established pods (was healthy): 1s, 2s, 5s, 10s, 20s (more conservative for known-good pods)
func quarantineBackoffSeconds(count uint32, wasPending bool) uint32 {
	if wasPending {
		// Shorter backoff for pods that are just starting up
		switch count {
		case 1:
			return 0 // Retry immediately on first failure for pending pods
		case 2:
			return 1 // ~500ms effective retry time
		case 3:
			return 1
		case 4:
			return 2
		default:
			return 5 // Cap at 5s for pending pods
		}
	}

	// Standard backoff for established pods that were previously healthy
	switch count {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 5
	case 4:
		return 10
	default:
		return 20
	}
}

// tcpPingCheck delegates to podReadyCheckFunc to allow for testing
// Note: Despite the name, this now performs HTTP health checks via podReadyCheck
func tcpPingCheck(dest string) bool {
	fn := podReadyCheckFunc.Load().(func(string) bool)
	return fn(dest)
}

// setPodReadyCheckFunc sets the health check function for testing
func setPodReadyCheckFunc(fn func(string) bool) {
	podReadyCheckFunc.Store(fn)
}
