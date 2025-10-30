/*
Copyright 2025 The Knative Authors

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
	"runtime/debug"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/exp/maps"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/uuid"
)

var (
	// podStateTransitions tracks pod state transitions
	// Labels: from_state, to_state, source (qp_push or k8s_informer)
	podStateTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pod_state_transitions_total",
			Help: "Total number of pod state transitions in activator",
		},
		[]string{"from_state", "to_state", "source"},
	)

	// stateUpdateQueueDepth tracks the current depth of the state update queue
	// This helps detect queue saturation and potential deadlock conditions
	// Labels: namespace, revision
	stateUpdateQueueDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "activator_revision_throttler_queue_depth",
			Help: "Current depth of state update queue per revision",
		},
		[]string{"namespace", "revision"},
	)

	// stateWorkerPanics tracks panics in the state worker goroutine
	// Labels: namespace, revision
	// High values indicate a bug that needs investigation
	stateWorkerPanics = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "activator_revision_throttler_worker_panics_total",
			Help: "Total number of panics in state worker goroutine (indicates bugs)",
		},
		[]string{"namespace", "revision"},
	)

	// stateUpdateProcessingTime tracks time spent processing state updates
	// Labels: namespace, revision, op_type
	// Helps identify performance bottlenecks in state management
	stateUpdateProcessingTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "activator_revision_throttler_processing_duration_seconds",
			Help:    "Time spent processing state update requests",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 0.1ms to ~100ms
		},
		[]string{"namespace", "revision", "op_type"},
	)

	// stateUpdateQueueTime tracks time requests spend waiting in queue
	// Labels: namespace, revision, op_type
	// Helps identify queue saturation and worker performance issues
	stateUpdateQueueTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "activator_revision_throttler_queue_duration_seconds",
			Help:    "Time state update requests spend waiting in queue before processing",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 0.1ms to ~100ms
		},
		[]string{"namespace", "revision", "op_type"},
	)
)

// stateWorker processes state update requests serially to prevent race conditions
// Buffer size: 500 requests can be queued before blocking senders
// This provides burst absorption while preventing unbounded memory growth
//
// Panic Recovery Behavior:
// - If a panic occurs during request processing, the worker restarts automatically
// - The in-flight request's done channel is closed to unblock the caller
// - This causes state loss for that single request
//
// State Loss Trade-off Rationale:
// - Alternative: Block system-wide until panic resolved → unacceptable (complete outage)
// - Lost state is recoverable: K8s informer will reconcile within seconds via opRecalculateAll
// - Single request loss is acceptable vs. blocking all revisions indefinitely
// - Panics are bugs that need fixing - tracked via stateWorkerPanics metric
// - Supervisor with exponential backoff prevents panic loops from cascading
func (rt *revisionThrottler) stateWorker() {
	var currentReq *stateUpdateRequest // Track in-flight request for panic recovery

	// Signal when worker exits (supervisor watches this)
	defer func() {
		if rt.workerDone != nil {
			close(rt.workerDone)
		}
	}()

	// Panic recovery - log and exit cleanly (supervisor will restart)
	defer func() {
		if r := recover(); r != nil {
			// Track panic time for health monitoring
			rt.lastPanicTime.Store(time.Now().Unix())

			// Increment panic counter for monitoring
			stateWorkerPanics.WithLabelValues(rt.revID.Namespace, rt.revID.Name).Inc()

			rt.logger.Errorw("State worker panicked - supervisor will restart",
				"panic", r,
				"stack", string(debug.Stack()))

			// Signal the in-flight request if any, so caller doesn't hang
			if currentReq != nil && currentReq.done != nil {
				safeCloseDone(currentReq.done)
				rt.logger.Warnw("In-flight request aborted due to panic - state loss occurred",
					"op", currentReq.op)
			}
			// Worker exits - supervisor will restart it
		}
	}()

	for {
		select {
		case <-rt.done:
			// Graceful shutdown with timeout
			shutdownTimer := time.NewTimer(workerShutdownTimeout)
			defer shutdownTimer.Stop()

			// Drain and process any remaining requests before exiting
			// This ensures state consistency during revision deletion
			for {
				select {
				case req := <-rt.stateUpdateChan:
					// Process the request to maintain state consistency
					currentReq = &req
					rt.processStateUpdate(req)
					currentReq = nil
					rt.onDequeueStateUpdate()
					// Signal completion if waiting
					safeCloseDone(req.done)
				case <-shutdownTimer.C:
					rt.logger.Warn("State worker shutdown timed out, forcing exit")
					return
				default:
					return
				}
			}
		case req := <-rt.stateUpdateChan:
			currentReq = &req // Track for panic recovery
			rt.processStateUpdate(req)
			currentReq = nil // Clear after successful processing

			// Update queue depth metric after processing
			rt.onDequeueStateUpdate()
			// Signal completion if waiting
			safeCloseDone(req.done)
		}
	}
}

// stateUpdateOp represents different types of state update operations
type stateUpdateOp int

const (
	opNoop                stateUpdateOp = iota // No-op, used for testing to ensure queue is drained
	opMutatePod                                // Adds, updates state, or transitions pod (based on event) AND updates capacity
	opRemovePod                                // Removes stale pod immediately (for IP reuse detection) AND updates capacity
	opRecalculateAll                           // Full reconciliation from K8s endpoints
	opRecalculateCapacity                      // Recalculate capacity only (activator assignment change)
	opCleanupStalePods                         // Cleanup stale podNotReady trackers (periodic background task)
)

// opTypeToString converts stateUpdateOp to string for metrics
func opTypeToString(op stateUpdateOp) string {
	switch op {
	case opNoop:
		return "noop"
	case opMutatePod:
		return "mutate_pod"
	case opRemovePod:
		return "remove_pod"
	case opRecalculateAll:
		return "recalculate_all"
	case opRecalculateCapacity:
		return "recalculate_capacity"
	case opCleanupStalePods:
		return "cleanup_stale_pods"
	default:
		return "unknown"
	}
}

// stateUpdateRequest represents a state mutation request to be processed serially
type stateUpdateRequest struct {
	op         stateUpdateOp
	pod        string           // Pod IP for pod operations
	eventType  string           // QP event type: "ready", "not-ready", "draining"
	dests      sets.Set[string] // K8s informer destinations for recalculation
	done       chan struct{}    // Optional channel to signal completion
	enqueuedAt int64            // Unix nanoseconds when request was enqueued (for queue time tracking)
}

// processStateUpdate handles a single state update request atomically
func (rt *revisionThrottler) processStateUpdate(req stateUpdateRequest) {
	opType := opTypeToString(req.op)

	// Track queue time (time spent waiting before processing)
	if req.enqueuedAt > 0 {
		queueTime := float64(time.Now().UnixNano()-req.enqueuedAt) / 1e9 // Convert to seconds
		stateUpdateQueueTime.WithLabelValues(rt.revID.Namespace, rt.revID.Name, opType).Observe(queueTime)
	}

	// Track processing time
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		stateUpdateProcessingTime.WithLabelValues(rt.revID.Namespace, rt.revID.Name, opType).Observe(duration)
	}()

	// Test-only panic injection hook
	// NOTE: This is intentionally placed BEFORE lock acquisition to ensure
	// panics during testing don't leave locks held. This is safe because
	// no state mutations have occurred yet at this point.
	if rt.testPanicInjector != nil {
		rt.testPanicInjector(req)
	}

	// Snapshot feature gates at operation start for consistency
	// This prevents mid-operation gate changes from causing inconsistent behavior
	qpAuthority, _ := getFeatureGates()

	// All operations hold the write lock for the entire duration
	// to ensure atomicity of pod mutations and capacity updates
	rt.mux.Lock()
	defer rt.mux.Unlock()

	switch req.op {
	case opMutatePod:
		rt.processMutatePod(req, qpAuthority)

	case opRemovePod:
		// Strict removal of stale tracker (IP reuse detected)
		// Remove immediately regardless of state or refCount
		rt.processRemovePod(req)

	case opRecalculateAll:
		// Full recalculation from K8s endpoints
		rt.recalculateFromEndpointsLocked(req.dests)
		rt.updateCapacityLocked()

	case opRecalculateCapacity:
		// Just recalculate capacity (activator assignment change)
		rt.updateCapacityLocked()

	case opCleanupStalePods:
		// Cleanup stale podNotReady trackers
		rt.processCleanupStalePods()

	case opNoop:
		// No-op, used for testing to ensure queue is drained
		// Nothing to do, just allows synchronization
	default:
		// This is an invalid operation, panic
		rt.logger.Fatalw("Received invalid operation",
			"pod-ip", req.pod,
			"event-type", req.eventType,
			"op-type", req.op)
	}
}

// processMutatePod handles adding a pod or updating its state based on QP events
// Must be called while holding the write lock
func (rt *revisionThrottler) processMutatePod(req stateUpdateRequest, qpAuthority bool) {
	// Check if pod already exists
	tracker, exists := rt.podTrackers[req.pod]

	if exists {
		// Pod exists - handle state update based on event type
		rt.handleExistingPodEvent(tracker, req.eventType, qpAuthority)
	} else {
		// Memory pressure protection: enforce max trackers per revision
		if len(rt.podTrackers) >= maxTrackersPerRevision {
			revisionTrackerLimitExceeded.WithLabelValues(rt.revID.Namespace, rt.revID.Name).Inc()
			rt.logger.Errorw("Rejected pod addition - max trackers exceeded",
				"pod", req.pod,
				"current-count", len(rt.podTrackers),
				"max-allowed", maxTrackersPerRevision)
			return
		}

		// Create new pod tracker
		tracker = &podTracker{
			dest:       req.pod,
			b:          rt.makeBreaker(),
			revisionID: rt.revID,
			id:         string(uuid.NewUUID()),
			createdAt:  time.Now().UnixMicro(),
			logger:     rt.logger,
		}

		// Set initial state based on event type and qpAuthority
		if qpAuthority {
			switch req.eventType {
			case "ready":
				tracker.state.Store(uint32(podReady))
				tracker.stateReason = "qp-ready"
			case "draining":
				// Pod is draining before ever being added - ignore it
				// This can happen if pod crashes during startup before QP sends ready event
				rt.logger.Warnw("Ignoring draining event for unknown pod - will never be ready",
					"pod", req.pod)
				return
			default:
				tracker.state.Store(uint32(podNotReady))
				tracker.stateReason = "qp-" + req.eventType // e.g. "qp-startup", "qp-not-ready"
			}
		} else {
			// QP authority disabled - start as ready
			tracker.state.Store(uint32(podReady))
			tracker.stateReason = "informer-added"
		}
		rt.podTrackers[req.pod] = tracker

		// Initialize QP tracking, done in all modes for logging and shadow deployments
		tracker.lastQPUpdate.Store(time.Now().Unix())
		tracker.lastQPState.Store(req.eventType)

		// Update capacity based on new pod count
		rt.updateCapacityLocked()

		rt.logger.Infow("Discovered new pod via push-based registration",
			"pod-ip", req.pod,
			"event-type", req.eventType,
			"initial-state", podState(tracker.state.Load()),
			"state-reason", tracker.stateReason)
	}
}

// processCleanupStalePods removes stale podNotReady trackers with zero refCount
// Must be called while holding the write lock
func (rt *revisionThrottler) processCleanupStalePods() {
	staleThresholdMicros := PodNotReadyStaleThreshold.Microseconds()
	now := time.Now().UnixMicro()

	for ip, tracker := range rt.podTrackers {
		state := podState(tracker.state.Load())
		refCount := tracker.refCount.Load()
		createdAt := tracker.createdAt

		// Cleanup podNotReady with zero refCount that are stale
		if state == podNotReady && refCount == 0 {
			ageMicros := now - createdAt
			if ageMicros > staleThresholdMicros {
				delete(rt.podTrackers, ip)
				rt.logger.Infow("Cleaned up stale podNotReady tracker",
					"revision", rt.revID.String(),
					"pod-ip", ip,
					"age-seconds", ageMicros/1000000, // Convert to seconds for logging
					"ref-count", refCount)
			}
		}
	}
	// Note: We don't call updateCapacityLocked() here because:
	// - These pods were already podNotReady (not routable)
	// - They were already excluded from capacity calculations
	// - No capacity change from removing them
}

// processRemovePod immediately removes a stale tracker from the map
// Must be called while holding the write lock
// This is used when IP reuse is detected - the tracker belongs to a different revision
// and must be removed immediately regardless of state or refCount
func (rt *revisionThrottler) processRemovePod(req stateUpdateRequest) {
	tracker, exists := rt.podTrackers[req.pod]
	if !exists {
		// Pod doesn't exist - this is a no-op (idempotent)
		rt.logger.Debugw("opRemovePod called for non-existent pod (no-op)",
			"pod-ip", req.pod)
		return
	}

	// Log removal with details for debugging
	state := podState(tracker.state.Load())
	refCount := tracker.refCount.Load()
	trackerRevision := tracker.revisionID.String()

	rt.logger.Warnw("Force removing stale tracker (IP reuse detected)",
		"pod-ip", req.pod,
		"tracker-revision", trackerRevision,
		"throttler-revision", rt.revID.String(),
		"tracker-id", tracker.id,
		"state", stateToString(state),
		"ref-count", refCount,
		"created-at", tracker.createdAt)

	// Handle quarantine metrics if pod is quarantined
	_, quarantineEnabled := getFeatureGates()
	if quarantineEnabled && state == podQuarantined {
		decrementQuarantineGauge(context.Background(), tracker)
	}

	// Remove from map immediately (regardless of refCount or state)
	delete(rt.podTrackers, req.pod)

	// Update capacity (pod is no longer counted)
	rt.updateCapacityLocked()
}

// onDequeueStateUpdate is called after processing a request to update metrics
func (rt *revisionThrottler) onDequeueStateUpdate() {
	stateUpdateQueueDepth.WithLabelValues(rt.revID.Namespace, rt.revID.Name).Set(float64(len(rt.stateUpdateChan)))
}

// enqueueStateUpdate sends a state update request to the worker queue.
// Blocks until the request is enqueued. Queue time is tracked via metrics.
// This ensures no state updates are dropped - if queue is full, caller blocks until space available.
func (rt *revisionThrottler) enqueueStateUpdate(req stateUpdateRequest) {
	// Set enqueue timestamp for queue time tracking
	req.enqueuedAt = time.Now().UnixNano()

	// Block until enqueued (no timeout - we never drop state updates)
	rt.stateUpdateChan <- req

	// Update queue depth metric
	stateUpdateQueueDepth.WithLabelValues(rt.revID.Namespace, rt.revID.Name).Set(float64(len(rt.stateUpdateChan)))
}

// handleExistingPodEvent handles QP events for existing pods
// Must be called while holding the write lock
func (rt *revisionThrottler) handleExistingPodEvent(tracker *podTracker, eventType string, qpAuthority bool) {
	// Always update QP freshness tracking
	tracker.lastQPUpdate.Store(time.Now().Unix())
	tracker.lastQPState.Store(eventType)

	// When QP authority is disabled, just log but don't change state
	if !qpAuthority {
		rt.logger.Debugw("Received QP event (QP authority disabled, no state change)",
			"pod-ip", tracker.dest,
			"event-type", eventType,
			"current-state", podState(tracker.state.Load()))
		return
	}

	oldState := podState(tracker.state.Load())
	oldRoutable := (oldState == podReady || oldState == podRecovering)
	stateChanged := false

	// Handle QP event types
	switch eventType {
	case "ready":
		if oldState == podNotReady {
			if tracker.state.CompareAndSwap(uint32(podNotReady), uint32(podReady)) {
				stateChanged = true
				tracker.stateReason = "qp-ready"
				rt.logger.Infow("QP promoted not-ready pod to ready",
					"pod-ip", tracker.dest,
					"state-reason", tracker.stateReason)
			}
		}

	case "not-ready":
		if oldState == podReady {
			if tracker.state.CompareAndSwap(uint32(podReady), uint32(podNotReady)) {
				stateChanged = true
				tracker.stateReason = "qp-not-ready"
				rt.logger.Warnw("QP demoted pod to not-ready (readiness probe failed)",
					"pod-ip", tracker.dest,
					"active-requests", tracker.refCount.Load(),
					"state-reason", tracker.stateReason)
			}
		}

	case "draining":
		// Transition to not-ready (stop routing, preserve active requests)
		switch oldState {
		case podReady, podRecovering:
			if tracker.state.CompareAndSwap(uint32(oldState), uint32(podNotReady)) {
				stateChanged = true
				tracker.stateReason = "qp-draining"
				rt.logger.Infow("QP initiated pod draining - transitioned to not-ready",
					"pod-ip", tracker.dest,
					"previous-state", oldState,
					"active-requests", tracker.refCount.Load(),
					"state-reason", tracker.stateReason)
			}
		case podNotReady:
			// Already not-ready (crash during startup) - update reason if needed
			if tracker.stateReason != "qp-draining" {
				tracker.stateReason = "qp-draining"
			}
			rt.logger.Debugw("QP draining event for already not-ready pod",
				"pod-ip", tracker.dest,
				"state-reason", tracker.stateReason)
		}
	default:
		// MUST be one of the above states; this branch is undefined behaviour
		rt.logger.Fatalw("Invalid state received",
			"pod-ip", tracker.dest,
			"qp-event", eventType)
	}

	// Only update capacity if routability changed
	if stateChanged {
		newState := podState(tracker.state.Load())
		newRoutable := (newState == podReady || newState == podRecovering)
		if oldRoutable != newRoutable {
			rt.updateCapacityLocked()
		}
		// Note: We don't remove pods here even with zero refCount during mutations
		// because mutate operations indicate state changes (ready/not-ready/draining)
		// and the pod might recover. The informer handles immediate removal when
		// K8s actually removes the pod from endpoints (see processRecalculateAll).
	}
}

// recalculateFromEndpointsLocked performs full reconciliation from K8s endpoints
// Must be called while holding the write lock
func (rt *revisionThrottler) recalculateFromEndpointsLocked(dests sets.Set[string]) {
	qpAuthority, quarantineEnabled := getFeatureGates()

	// This reconciles the pod tracker map with the destinations from K8s informer
	// It replicates the logic from the original updateThrottlerState

	// First, identify which pods are new, which remain, and which are gone
	currentDests := maps.Keys(rt.podTrackers)

	// Create new trackers for pods that don't exist yet
	for dest := range dests {
		if _, exists := rt.podTrackers[dest]; !exists {
			// Create new tracker
			tracker := &podTracker{
				dest:       dest,
				b:          rt.makeBreaker(),
				revisionID: rt.revID,
				id:         string(uuid.NewUUID()),
				createdAt:  time.Now().UnixMicro(),
				logger:     rt.logger,
			}

			// When QP authority is enabled, start as podNotReady (wait for QP ready event)
			// When disabled, start as podReady (trust K8s immediately)
			if qpAuthority {
				tracker.state.Store(uint32(podNotReady))
				tracker.stateReason = "informer-pending"

				// Check if we should immediately promote to ready
				// (QP has never been heard from, so it's considered stale)
				lastQPSeen := tracker.lastQPUpdate.Load()
				qpAge := time.Now().Unix() - lastQPSeen

				if qpAge > int64(QPStalenessThreshold.Seconds()) {
					// QP data is stale (or never received), trust K8s informer
					tracker.state.Store(uint32(podReady))
					tracker.stateReason = "informer-ready"
					rt.logger.Debugw("New pod tracker immediately promoted to ready (no QP data)",
						"dest", dest,
						"tracker-id", tracker.id,
						"qp-age-sec", qpAge,
						"state-reason", tracker.stateReason)
				}
			} else {
				tracker.state.Store(uint32(podReady))
				tracker.stateReason = "informer-ready"
			}

			rt.podTrackers[dest] = tracker
			rt.logger.Debugw("Created new pod tracker from K8s informer",
				"dest", dest,
				"tracker-id", tracker.id,
				"initial-state", podState(tracker.state.Load()),
				"state-reason", tracker.stateReason)
		} else {
			// Pod exists - handle state based on QP authority mode
			tracker := rt.podTrackers[dest]
			currentState := podState(tracker.state.Load())

			// Check QP freshness to determine if we should trust informer (only when QP authority enabled)
			if qpAuthority {
				lastQPSeen := tracker.lastQPUpdate.Load()
				qpAge := time.Now().Unix() - lastQPSeen

				var lastQPEvent string
				if val := tracker.lastQPState.Load(); val != nil {
					if s, ok := val.(string); ok {
						lastQPEvent = s
					}
				}

				// Handle pending pods
				if currentState == podNotReady {
					// Only promote to ready if QP data is stale or QP confirmed ready
					if qpAge > int64(QPStalenessThreshold.Seconds()) || lastQPEvent == "ready" {
						tracker.state.Store(uint32(podReady))
						tracker.stateReason = "informer-promoted"
						podStateTransitions.WithLabelValues("not-ready", "ready", "k8s_informer").Inc()
						rt.logger.Infow("K8s promoting pod from not-ready to ready (QP data stale or confirmed)",
							"dest", dest,
							"qp-age-sec", qpAge,
							"last-qp-event", lastQPEvent,
							"state-reason", tracker.stateReason)
					}
				}
			}
			// Note: Other state transitions handled below for removed pods
		}
	}

	// Handle pods that are no longer in the K8s endpoints
	for _, dest := range currentDests {
		if !dests.Has(dest) {
			// Pod is being removed
			tracker := rt.podTrackers[dest]
			if tracker != nil {
				currentState := podState(tracker.state.Load())

				switch currentState {
				case podReady, podRecovering:
					// When QP authority is enabled, check if we should ignore K8s removal
					// based on fresh QP data
					if qpAuthority && currentState == podReady {
						qpInfo := tracker.getQPFreshness()
						// If QP recently confirmed "ready", ignore K8s removal signal
						if qpInfo.lastEvent == "ready" && qpInfo.age < int64(QPFreshnessReadyWindow.Seconds()) {
							rt.logger.Debugw("Ignoring K8s removal - QP authority overrides (fresh ready signal)",
								"dest", dest,
								"qp-age-sec", qpInfo.age,
								"freshness-window-sec", int64(QPFreshnessReadyWindow.Seconds()))
							continue // Keep the pod, don't remove it
						}
					}

					// Transition to not-ready (stop routing, preserve active requests)
					if tracker.state.CompareAndSwap(uint32(currentState), uint32(podNotReady)) {
						tracker.stateReason = "informer-removed"
						fromState := "ready"
						if currentState == podRecovering {
							fromState = "recovering"
						}
						podStateTransitions.WithLabelValues(fromState, "not-ready", "k8s_informer").Inc()
						rt.logger.Debugf("Pod %s transitioning to not-ready (K8s removing), refCount=%d, reason=%s",
							dest, tracker.getRefCount(), tracker.stateReason)

						// If no active requests, remove immediately
						if tracker.getRefCount() == 0 {
							delete(rt.podTrackers, dest)
							rt.logger.Debugf("Pod %s removed immediately (no active requests)", dest)
						}
					}
				case podNotReady:
					// Already not-ready, check if can be removed
					if tracker.getRefCount() == 0 {
						delete(rt.podTrackers, dest)
						rt.logger.Debugf("Pod %s removed (no active requests)", dest)
					}
				case podQuarantined:
					// Decrement quarantine gauge if enabled
					if quarantineEnabled {
						count := decrementQuarantineGauge(context.Background(), tracker)
						rt.logger.Infow("Removing quarantined pod", "dest", dest, "quarantine-count", count)
					}
					delete(rt.podTrackers, dest)
				default:
					rt.logger.Fatalw("Pod in unexpected state while processing removal",
						"dest", dest,
						"state", currentState,
						"valid-states", []string{"podReady", "podRecovering", "podNotReady", "podQuarantined"})
				}
			}
		}
	}
}
