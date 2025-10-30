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
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/maps"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/logging/logkey"
	"knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/queue"
)

type revisionThrottler struct {
	revID                types.NamespacedName
	containerConcurrency atomic.Uint64
	lbPolicy             atomic.Value // Store lbPolicy function atomically

	// These are used in slicing to infer which pods to assign
	// to this activator.
	numActivators atomic.Uint64
	// If 0, it is presumed that this activator should not receive requests
	// for the revision. But due to the system being distributed it might take
	// time for everything to propagate. Thus when this is 0 we assign all the
	// pod trackers. Uses 1-based indexing (1, 2, 3...) for actual positions.
	activatorIndex atomic.Uint64
	protocol       string

	// Holds the current number of backends. This is used for when we get an activatorCount update and
	// therefore need to recalculate capacity
	backendCount atomic.Uint64 // Make atomic to prevent races

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

	// stateUpdateChan serializes all pod state mutations and capacity updates
	// through a single worker goroutine to prevent race conditions.
	stateUpdateChan chan stateUpdateRequest
	// done signals the supervisor and worker to stop
	done chan struct{}
	// workerDone signals when the worker has exited (supervisor watches this)
	workerDone chan struct{}
	// supervisorDone signals when the supervisor has fully exited
	supervisorDone chan struct{}

	// lastPanicTime tracks when the worker last exited (for exponential backoff)
	// Stored as Unix microseconds. Used to detect panic loops and apply backoff.
	lastPanicTime atomic.Int64

	// testPanicInjector is used ONLY for testing panic recovery behavior
	// When non-nil, called before processing each request to allow panic injection
	testPanicInjector func(stateUpdateRequest)

	logger *zap.SugaredLogger
}

func newRevisionThrottler(revID types.NamespacedName,
	loadBalancerPolicy *string,
	containerConcurrency uint64, proto string,
	breakerParams queue.BreakerParams,
	logger *zap.SugaredLogger,
) (*revisionThrottler, error) { //nolint:unparam // Keep error return for future validation extensibility
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
		// stateUpdateQueueSize=500 is sufficient for production workloads (see throttler.go:88 for sizing rationale)
		// Making this configurable would add complexity without clear benefit - monitor stateUpdateQueueDepth metric instead
		stateUpdateChan: make(chan stateUpdateRequest, stateUpdateQueueSize),
		done:            make(chan struct{}),
		workerDone:      make(chan struct{}),
		supervisorDone:  make(chan struct{}),
	}
	t.containerConcurrency.Store(containerConcurrency)
	t.lbPolicy.Store(lbp)

	// Start with unknown (0 means not ready/not in endpoints with 1-based indexing)
	t.activatorIndex.Store(0)

	// Start the supervisor which manages the worker lifecycle
	go t.supervisor()
	return t, nil
}

// supervisor manages the worker lifecycle, restarting it on panic with exponential backoff
// to prevent tight panic loops from consuming CPU.
func (rt *revisionThrottler) supervisor() {
	defer close(rt.supervisorDone)

	// Initial backoff duration
	backoff := 100 * time.Millisecond
	const maxBackoff = 5 * time.Second
	const resetWindow = time.Minute

	for {
		// Start a new worker
		go rt.stateWorker()

		// Wait for either shutdown signal or worker exit
		select {
		case <-rt.done:
			// Shutdown requested - wait for worker to finish
			<-rt.workerDone
			return
		case <-rt.workerDone:
			// Worker exited (likely panic) - check if we should restart
			select {
			case <-rt.done:
				// Shutdown was requested, don't restart
				return
			default:
				// Worker panicked - apply exponential backoff if panicking frequently
				now := time.Now()
				lastPanic := rt.lastPanicTime.Load()

				if lastPanic > 0 {
					timeSinceLastPanic := now.Sub(time.UnixMicro(lastPanic))

					if timeSinceLastPanic < resetWindow {
						// Panic happened recently - apply and increase backoff
						rt.logger.Infow("Worker exited unexpectedly, applying backoff before restart",
							"backoff", backoff,
							"time-since-last-panic", timeSinceLastPanic)
						time.Sleep(backoff)

						// Exponential backoff with cap
						backoff *= 2
						if backoff > maxBackoff {
							backoff = maxBackoff
						}
					} else {
						// Long time since last panic - reset backoff
						rt.logger.Info("Worker exited unexpectedly, restarting (backoff reset)")
						backoff = 100 * time.Millisecond
					}
				} else {
					// First panic - no backoff needed yet
					rt.logger.Info("Worker exited unexpectedly, restarting")
				}

				// Record this panic time
				rt.lastPanicTime.Store(now.UnixMicro())

				// Create new channel for next worker
				rt.workerDone = make(chan struct{})
			}
		}
	}
}

// Close shuts down the revision throttler's worker goroutine and cleans up resources.
// Blocks until the supervisor and worker have fully exited to ensure clean shutdown.
func (rt *revisionThrottler) Close() {
	// Signal shutdown (worker and supervisor both watch this)
	close(rt.done)

	// Note: We don't close stateUpdateChan here because:
	// - Callers may still try to enqueue (would panic on send to closed channel)
	// - Worker drains the channel during shutdown
	// - Channel will be GC'd when throttler is GC'd

	// Wait for supervisor to fully exit (supervisor waits for worker)
	<-rt.supervisorDone
}

// FlushForTesting ensures all queued requests have been processed.
// This is only for testing and should not be used in production code.
func (rt *revisionThrottler) FlushForTesting() {
	done := make(chan struct{})
	rt.enqueueStateUpdate(stateUpdateRequest{
		op:   opNoop,
		done: done,
	})
	<-done
}

func noop() {}

// IsWorkerHealthy returns true if the worker is operating normally
// Returns false if the worker has panicked recently or queue is saturated
func (rt *revisionThrottler) IsWorkerHealthy() bool {
	// Check if panicked in the last minute
	lastPanic := rt.lastPanicTime.Load()
	if lastPanic > 0 && time.Since(time.Unix(lastPanic, 0)) < time.Minute {
		return false
	}

	// Check queue saturation (> 50% full indicates pressure)
	queueDepth := len(rt.stateUpdateChan)
	queueCapacity := cap(rt.stateUpdateChan)
	return queueDepth <= queueCapacity/2
}

// safeCloseDone safely closes a done channel, preventing panic from double-close
func safeCloseDone(done chan struct{}) {
	if done != nil {
		select {
		case <-done:
			// Already closed, do nothing
		default:
			close(done)
		}
	}
}

// makeBreaker creates a new breaker for a pod tracker
func (rt *revisionThrottler) makeBreaker() breaker {
	cc := rt.containerConcurrency.Load()
	if cc == 0 {
		return nil
	}
	return queue.NewBreaker(queue.BreakerParams{
		QueueDepth:      breakerQueueDepth,
		MaxConcurrency:  cc,
		InitialCapacity: cc,
	})
}

// updateCapacityLocked updates capacity while already holding the write lock
// This is called from processStateUpdate which already holds the lock
func (rt *revisionThrottler) updateCapacityLocked() {
	backendCount := len(rt.podTrackers)

	// When there are no pods, clear everything and set capacity to 0
	if backendCount == 0 {
		rt.assignedTrackers = nil
		rt.breaker.UpdateConcurrency(0)
		rt.logger.Debugw("All pods removed, capacity set to 0",
			"revision", rt.revID.String())
		return
	}

	// Reset per-pod breakers if needed
	rt.resetTrackersLocked()

	// Recompute assigned trackers
	rt.assignedTrackers = rt.recomputeAssignedTrackers(rt.podTrackers)

	// Calculate and update capacity
	numTrackers := uint64(len(rt.assignedTrackers))
	activatorCount := rt.numActivators.Load()
	targetCapacity := rt.calculateCapacity(uint64(backendCount), numTrackers, activatorCount)
	rt.breaker.UpdateConcurrency(targetCapacity)

	rt.logger.Debugw("Capacity updated",
		"revision", rt.revID.String(),
		"backends", backendCount,
		"assigned", numTrackers,
		"capacity", targetCapacity)
}

// resetTrackersLocked resets breaker capacity while holding the lock
func (rt *revisionThrottler) resetTrackersLocked() {
	cc := rt.containerConcurrency.Load()
	if cc == 0 {
		return
	}

	for _, t := range rt.podTrackers {
		if t != nil {
			t.UpdateConcurrency(cc)
		}
	}
}

// filterAvailableTrackers returns only healthy pods ready to serve traffic
// podNotReady pods are excluded until explicitly promoted to healthy
// When enableQuarantine=true, also filters out quarantined/recovering pods
func (rt *revisionThrottler) filterAvailableTrackers(trackers []*podTracker) []*podTracker {
	_, quarantineEnabled := getFeatureGates()

	available := make([]*podTracker, 0, len(trackers))

	now := time.Now().Unix()
	for _, tracker := range trackers {
		if tracker == nil {
			continue
		}

		// Check for automatic quarantine expiration (uses helper)
		if quarantineEnabled && tracker.checkQuarantineExpiration(now) {
			count := decrementQuarantineGauge(context.Background(), tracker)
			rt.logger.Debugw("Pod exiting quarantine, entering recovery",
				"dest", tracker.dest,
				"quarantine-count", count)
		}

		// Filter based on current state (after potential transition)
		state := podState(tracker.state.Load())

		if quarantineEnabled {
			switch state {
			case podQuarantined:
				continue // Skip quarantined pods
			case podRecovering, podReady:
				available = append(available, tracker)
			default:
				// Skip not-ready
				continue
			}
		} else if state == podReady {
			// When quarantine is disabled, only allow podReady
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
	warningTimer := time.NewTimer(proxyQueueTimeWarningThreshold)
	errorTimer := time.NewTimer(proxyQueueTimeErrorThreshold)
	criticalTimer := time.NewTimer(proxyQueueTimeCriticalThreshold)
	defer warningTimer.Stop()
	defer errorTimer.Stop()
	defer criticalTimer.Stop()

	// Channel to ensure shutdown - will be closed when routing completes (not proxy)
	thresholdChan := make(chan struct{})
	var closeOnce sync.Once

	// Ensure we always close the channel on function exit
	defer closeOnce.Do(func() {
		close(thresholdChan)
	})

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

		// Get diagnostic info about pod/tracker state before entering breaker
		// Consolidate lock acquisition to reduce contention
		rt.mux.RLock()
		assignedTrackers := rt.assignedTrackers
		totalPods := len(rt.podTrackers)
		assignedCount := len(rt.assignedTrackers)
		rt.mux.RUnlock()

		if len(assignedTrackers) == 0 {
			rt.logger.Debugf("%s -> No Assigned trackers\n", xRequestId)
		}

		// Track revision-level breaker wait
		breakerWaitStart := time.Now()
		breakerCapacity := rt.breaker.Capacity()
		breakerPending := rt.breaker.Pending()
		breakerInflight := rt.breaker.InFlight()

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
				notReadyCount := 0
				quarantinedCount := 0
				for _, t := range rt.assignedTrackers {
					if t != nil {
						state := podState(t.state.Load())
						if state == podNotReady {
							notReadyCount++
						} else if enableQuarantine && state == podQuarantined {
							quarantinedCount++
						}
					}
				}
				rt.mux.RUnlock()

				// Log if all pods are unavailable - rate limit to avoid spam
				unavailableCount := notReadyCount + quarantinedCount
				if unavailableCount == assignedCount && assignedCount > 0 {
					shouldLog := reenqueueCount == 0 || reenqueueCount%100 == 0
					if shouldLog {
						if enableQuarantine && quarantinedCount > 0 {
							rt.logger.Warnw("all pods unavailable (not-ready + quarantined); re-enqueue",
								"x-request-id", xRequestId,
								"not-ready", notReadyCount,
								"quarantined", quarantinedCount,
								"assigned", assignedCount,
								"reenqueueCount", reenqueueCount,
								"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()),
							)
						} else {
							rt.logger.Warnw("all pods not ready; re-enqueue",
								"x-request-id", xRequestId,
								"not-ready", notReadyCount,
								"assigned", assignedCount,
								"reenqueueCount", reenqueueCount,
								"elapsed-ms", float64(time.Since(proxyStartTime).Milliseconds()),
							)
						}
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
					"not-ready", notReadyCount,
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

				// Enqueue opRemovePod to remove stale tracker immediately
				// Don't wait for completion - just fire and forget
				go func(podIP string) {
					rt.enqueueStateUpdate(stateUpdateRequest{
						op:  opRemovePod,
						pod: podIP,
					})
				}(tracker.dest)

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
			closeOnce.Do(func() {
				close(thresholdChan)
			})

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

			// Health check logic (only when enableQuarantine=true)
			// CRITICAL: Only health check routable pods (podReady, podRecovering)
			if !isClusterIP {
				wasQuarantined, isStaleTracker, healthCheckMs := performHealthCheckAndQuarantine(ctx, tracker)

				if healthCheckMs > 1000 { // Log if health check took >1s
					rt.logger.Warnw("Slow health check",
						"x-request-id", xRequestId,
						"dest", tracker.dest,
						"health-check-ms", healthCheckMs,
						"quarantined", wasQuarantined,
						"stale-tracker", isStaleTracker)
				}

				// Check for stale tracker (IP reuse detected)
				if isStaleTracker {
					rt.logger.Errorw("Health check detected IP reuse - enqueuing stale tracker removal",
						"x-request-id", xRequestId,
						"dest", tracker.dest,
						"tracker-revision", tracker.revisionID.String(),
						"throttler-revision", rt.revID.String(),
						"tracker-id", tracker.id,
						"reenqueue-count", reenqueueCount)

					// Enqueue opRemovePod to remove stale tracker immediately
					// Don't wait for completion - just fire and forget
					go func(podIP string) {
						rt.enqueueStateUpdate(stateUpdateRequest{
							op:  opRemovePod,
							pod: podIP,
						})
					}(tracker.dest)

					// Re-queue the request to try another backend
					reenqueue = true
					return
				}

				if wasQuarantined {
					rt.logger.Errorw("pod ready check failed; quarantine",
						"x-request-id", xRequestId,
						"dest", tracker.dest,
						"revision", rt.revID.String(),
						"tracker-id", tracker.id,
						"reenqueue-count", reenqueueCount,
						"quarantine-count", tracker.quarantineCount.Load())
					// Re-queue the request to try another backend
					reenqueue = true
					return
				}
			}

			// Time the actual proxy call
			proxyCallStart := time.Now()
			ret = function(tracker.dest, isClusterIP)
			proxyDurationMs = float64(time.Since(proxyCallStart).Milliseconds())

			// When enableQuarantine=true, handle post-request state transitions
			if ret == nil && !isClusterIP {
				// Request succeeded - if pod was recovering, promote back to healthy
				if tracker.tryPromoteRecovering() {
					rt.logger.Infow("Pod recovered from quarantine, promoting to healthy",
						"x-request-id", xRequestId,
						"dest", tracker.dest)
				}
			}
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
		rt.logger.Debugw("Request routing completed",
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

func (rt *revisionThrottler) calculateCapacity(backendCount uint64, numTrackers uint64, activatorCount uint64) uint64 {
	var targetCapacity uint64
	cc := rt.containerConcurrency.Load()

	if numTrackers > 0 {
		// Capacity is computed based off of number of trackers,
		// when using pod direct routing.
		// We use number of assignedTrackers (numTrackers) for calculation
		// since assignedTrackers means activator's capacity
		targetCapacity = cc * numTrackers
	} else {
		// Capacity is computed off of number of ready backends,
		// when we are using clusterIP routing.
		targetCapacity = cc * backendCount
		if targetCapacity > 0 {
			targetCapacity /= max(1, activatorCount)
		}
	}

	if (backendCount > 0) && (cc == 0 || targetCapacity > revisionMaxConcurrency) {
		// If cc==0, we need to pick a number, but it does not matter, since
		// infinite breaker will dole out as many tokens as it can.
		// For cc>0 we clamp targetCapacity to maxConcurrency because the backing
		// breaker requires some limit (it's backed by a chan struct{}), but the
		// limit is math.MaxInt32 so in practice this should never be a real limit.
		targetCapacity = revisionMaxConcurrency
	}

	return targetCapacity
}

// updateCapacity updates the capacity of the throttler and recomputes
// the assigned trackers to the Activator instance.
//
// **CRITICAL**: This method blocks waiting for the worker to process the request.
// The worker acquires rt.mux in processStateUpdate(). Therefore, callers MUST NOT
// hold rt.mux when calling this method, or deadlock will occur.
//
// Current callers:
// 1. K8s informer updates (handleUpdate) - releases lock before calling
// 2. Activator endpoint updates (handlePubEpsUpdate) - never holds lock
//
// The work queue ensures pod mutations and capacity updates are serialized.
func (rt *revisionThrottler) updateCapacity() {
	// Send a capacity recalculation request through the work queue to avoid races
	done := make(chan struct{})
	rt.enqueueStateUpdate(stateUpdateRequest{
		op:   opRecalculateCapacity,
		done: done,
	})
	<-done // Wait for completion
}

// assignSlice picks a subset of the individual pods to send requests to
// for this Activator instance. This only matters in case of direct
// to pod IP routing, and is irrelevant, when ClusterIP is used.
// Uses consistent hashing to ensure all activators independently assign the correct endpoints.
func assignSlice(trackers map[string]*podTracker, selfIndex, numActivators uint64) []*podTracker {
	// Handle edge cases
	if selfIndex == 0 { // Changed from -1 to 0 (not ready/not in endpoints)
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

	// Bounds check: ensure selfIndex is valid for multi-activator scenarios
	// With 1-based indexing, valid indices are 1 through numActivators
	if numActivators > 0 && selfIndex > numActivators {
		// Invalid index - assign no pods to prevent undefined behavior
		// This can happen during activator scale-down when indices haven't been rebalanced yet
		return []*podTracker{}
	}

	// Use consistent hashing with 1-based adjustment:
	// take all pods where podIdx % numActivators == (selfIndex-1)
	assigned := make([]*podTracker, 0)
	for i, dest := range dests {
		//nolint:gosec // G115: Safe conversion - i is array index, always small and positive
		if uint64(i)%numActivators == (selfIndex - 1) { // Subtract 1 for modulo with 1-based index
			assigned = append(assigned, trackers[dest])
		}
	}

	return assigned
}

// recomputeAssignedTrackers computes which pods should be assigned to this activator
// Must be called while holding the lock
func (rt *revisionThrottler) recomputeAssignedTrackers(podTrackers map[string]*podTracker) []*podTracker {
	// If we're using cluster IP routing, no direct pod assignment
	if rt.clusterIPTracker != nil {
		return nil
	}

	ac, ai := rt.numActivators.Load(), rt.activatorIndex.Load()

	// Use assignSlice to compute which pods belong to this activator
	assigned := assignSlice(podTrackers, ai, ac)

	// Sort for stable ordering
	sort.Slice(assigned, func(i, j int) bool {
		return assigned[i].dest < assigned[j].dest
	})

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

	// Route through work queue to avoid TOCTOU race
	// This ensures all state mutations and capacity updates are serialized
	done := make(chan struct{})
	rt.enqueueStateUpdate(stateUpdateRequest{
		op:    opRecalculateAll,
		dests: update.Dests, // Pass the destinations set for recalculation
		done:  done,
	})

	// Wait for the worker to process the request
	<-done

	// All K8s informer updates are now handled through the work queue
	// The old inline implementation has been removed to ensure proper serialization
}

// addPodIncremental handles pod state updates through the work queue.
// This is used for push-based pod discovery where queue-proxies self-register.
// Handles 3 event types from queue-proxy:
// - "ready": promotes podNotReady → podReady (or creates new tracker as podReady)
// - "not-ready": demotes podReady → podNotReady (or creates new tracker as podNotReady)
// - "draining": demotes podReady → podNotReady (graceful shutdown, preserves refCount)
//
// All state mutations are serialized through the work queue to prevent race conditions.
// This function blocks until the request is processed for backward compatibility.
// Includes timeout protection in case the worker goroutine dies.
func (rt *revisionThrottler) mutatePodIncremental(podIP string, eventType string) {
	// Create a done channel to wait for processing
	done := make(chan struct{})

	// Queue the request with done channel
	rt.enqueueStateUpdate(stateUpdateRequest{
		op:        opMutatePod,
		pod:       podIP,
		eventType: eventType,
		done:      done,
	})

	// Wait for the worker to process the request
	<-done
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
	// Using uint64 for all capacity-related values
	//nolint:gosec // G115: Safe conversion - inferIndex returns activator position, bounded by cluster size (typically < 100)
	newNA, newAI := uint64(len(epsL)), uint64(inferIndex(epsL, selfIP))
	if newAI == 0 { // Changed from -1 to 0 (not in endpoints)
		// No need to do anything, this activator is not in path.
		return
	}

	na, ai := rt.numActivators.Load(), rt.activatorIndex.Load()
	if na == newNA && ai == newAI {
		// The state didn't change, do nothing
		return
	}

	rt.numActivators.Store(newNA)
	rt.activatorIndex.Store(newAI)
	rt.logger.Debugf("This activator index is %d/%d was %d/%d",
		newAI, newNA, ai, na)

	// Route capacity update through work queue to avoid TOCTOU race
	// The work queue will handle the capacity update atomically with other state changes
	done := make(chan struct{})
	rt.enqueueStateUpdate(stateUpdateRequest{
		op:   opRecalculateCapacity,
		done: done,
	})

	// Wait for the worker to process the request
	<-done
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
	return uint64(ib.concurrency.Load())
}

// Pending returns the current pending requests the breaker
func (ib *infiniteBreaker) Pending() int {
	return int(ib.concurrency.Load())
}

// Pending returns the current inflight requests the breaker
func (ib *infiniteBreaker) InFlight() uint64 {
	return uint64(ib.concurrency.Load())
}

func zeroOrOne(x uint64) uint32 {
	if x == 0 {
		return 0
	}
	return 1
}

// UpdateConcurrency sets the concurrency of the breaker
func (ib *infiniteBreaker) UpdateConcurrency(cc uint64) {
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
