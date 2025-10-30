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
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"knative.dev/pkg/logging"
)

// Pod State Machine Documentation
//
// The activator implements a 4-state pod state machine with two independent feature gates:
// - enableQPAuthority (default: true): Controls whether queue-proxy events trigger state changes
// - enableQuarantine (default: true): Controls health-check and quarantine system
//
// These gates can be configured via config-features ConfigMap:
// - queueproxy.pod-authority: enabled/disabled
// - activator.pod-quarantine: enabled/disabled
//
// ============================================================================
// STATE MACHINE DIAGRAM (All Features Enabled - Hybrid Mode)
// ============================================================================
//
// NEW POD → podNotReady ──QP "ready"──→ podReady ←──health check fails──→ podQuarantined
//              ↑                           │                                      │
//              │                           │                                      │
//              │                    QP "not-ready"                         after backoff
//              │                    QP "draining"                                 ↓
//              │                    K8s removal                            podRecovering
//              │                           │                                      │
//              │                           ↓                                      │
//              └──────────────────────podNotReady ←──health check fails────────┘
//                                          │
//                                   refCount → 0
//                                          ↓
//                              delete from podTrackers map
//                                          │
//                             (Re-add from K8s informer)
//                                          ↓
//                                    podNotReady
//
// ============================================================================
// OPERATING MODES (Based on Feature Gate Combinations)
// ============================================================================
//
// Mode 1: HYBRID (QP Authority ON, Quarantine ON) - Production Default
// ────────────────────────────────────────────────────────────────────
// Both systems active simultaneously. QP events control base state,
// health checks can quarantine failing pods. Most comprehensive protection.
//
// States used: All 4 states
// Initial state: podNotReady (promoted by QP "ready" event)
// State transitions:
//   - NEW POD → podNotReady (waiting for QP ready signal)
//   - QP "ready" → podReady (pod now routable)
//   - QP "not-ready" → podNotReady (pod stops receiving traffic but preserves refCount)
//   - QP "draining" → podNotReady (graceful shutdown, preserves refCount)
//   - K8s removal → podNotReady (stop routing, preserve active requests)
//   - Health check failure → podQuarantined (temporary quarantine)
//   - After backoff → podRecovering (re-check health)
//   - Health check success → podReady (back to normal)
//   - refCount → 0 → delete from map (cleanup)
//
// Mode 2: QP AUTHORITY ONLY (QP Authority ON, Quarantine OFF)
// ────────────────────────────────────────────────────────────
// Queue-proxy events are authoritative, no health checks.
//
// States used: podReady, podNotReady
// Initial state: podNotReady (promoted by QP "ready" event)
// State transitions:
//   - NEW POD → podNotReady (waiting for QP ready signal)
//   - QP "ready" → podReady
//   - QP "not-ready" → podNotReady
//   - QP "draining" → podNotReady
//   - K8s removal → podNotReady
//   - refCount → 0 → delete from map
//
// Mode 3: QUARANTINE ONLY (QP Authority OFF, Quarantine ON)
// ──────────────────────────────────────────────────────────
// K8s informer is sole authority, health checks run and can quarantine.
//
// States used: podReady, podQuarantined, podRecovering, podNotReady
// Initial state: podReady (K8s informer controls promotion)
// State transitions:
//   - NEW POD → podReady (trust K8s immediately)
//   - Health check failure → podQuarantined
//   - After backoff → podRecovering
//   - Health check success → podReady
//   - K8s "not ready" → podNotReady
//   - K8s removal → delete from map
//
// Mode 4: MINIMAL (Both OFF)
// ───────────────────────────
// K8s informer only, no health checks, no QP authority.
//
// States used: podReady, podNotReady
// Initial state: podReady (K8s informer controls everything)
// State transitions:
//   - NEW POD → podReady (trust K8s)
//   - K8s "not ready" → podNotReady
//   - K8s removal → delete from map
//
// ============================================================================
// STATE DESCRIPTIONS
// ============================================================================
//
// podReady (0): Pod is healthy and ready to serve traffic
//   - Routable: YES
//   - Can receive new requests: YES
//   - Active requests preserved: N/A
//   - Exit conditions:
//     * QP "not-ready" event (when enableQPAuthority=true) → podNotReady
//     * QP "draining" event (when enableQPAuthority=true) → podNotReady
//     * Health check failure (when enableQuarantine=true) → podQuarantined
//     * K8s informer removal → podNotReady
//
// podQuarantined (1): Pod failed health check and is temporarily quarantined
//   - Routable: NO (only used when enableQuarantine=true)
//   - Can receive new requests: NO
//   - Active requests preserved: YES (existing requests complete)
//   - Entry conditions: Health check failure from podReady or podRecovering
//   - Exit conditions:
//     * After backoff period → podRecovering (re-check health)
//     * K8s informer removal → delete from map
//
// podRecovering (2): Pod recovering from quarantine, being re-tested
//   - Routable: YES (only used when enableQuarantine=true)
//   - Can receive new requests: YES (limited during recovery)
//   - Active requests preserved: YES
//   - Entry conditions: After quarantine backoff expires
//   - Exit conditions:
//     * Health check success → podReady (recovered)
//     * Health check failure → podQuarantined (back to quarantine)
//     * QP "draining" → podNotReady
//     * K8s informer removal → podNotReady
//
// podNotReady (3): Pod exists but not ready to receive traffic
//   - Routable: NO (excluded from routing pool)
//   - Can receive new requests: NO
//   - Active requests preserved: YES (refCount from previous podReady state)
//   - Entry conditions:
//     * New pod creation (initial state when enableQPAuthority=true)
//     * QP "not-ready" event (demotion from podReady)
//     * QP "startup" event (explicit not-ready signal)
//     * QP "draining" event (graceful shutdown)
//     * K8s informer "not ready" (when QP not authoritative)
//     * K8s informer removal (stop routing, preserve active requests)
//   - Exit conditions:
//     * QP "ready" event → podReady (promotion when enableQPAuthority=true)
//     * K8s informer "healthy" → podReady (when QP data stale or disabled)
//     * refCount → 0 → delete from map (cleanup)
//
// ============================================================================
// QUEUE-PROXY AUTHORITY MODEL (when enableQPAuthority=true)
// ============================================================================
//
// Trust Hierarchy:
// 1. Queue-Proxy Push (AUTHORITATIVE when enabled) - Pod knows its own state best
//    - QP data < 30s old: QP state overrides K8s informer
//    - QP events: "startup", "ready", "not-ready", "draining"
//
// 2. K8s Informer (FALLBACK) - Trusted when QP silent or gate disabled
//    - QP data > 60s old: Trust informer (QP likely dead/crashed)
//    - QP never heard from: Informer is sole authority
//    - enableQPAuthority=false: Informer is always authoritative
//
// QP Events (require enableQPAuthority=true to trigger state changes):
//   - "startup": Creates podNotReady tracker (not viable for traffic)
//   - "ready": Promotes podNotReady → podReady (now viable)
//   - "not-ready": Demotes podReady → podNotReady (stops new traffic, preserves active requests)
//   - "draining": Demotes podReady → podNotReady (graceful shutdown, preserves active requests)
//
// Informer Override Rules (when enableQPAuthority=true):
//   - ✅ QP "not-ready" < 30s ago → Informer "healthy" IGNORED
//   - ✅ QP "ready" < 30s ago → Informer "draining" IGNORED
//   - ✅ QP silent > 60s → Informer is AUTHORITATIVE (QP likely dead)
//   - ✅ No QP data → Informer is AUTHORITATIVE
//
// ============================================================================
// CRITICAL INVARIANTS
// ============================================================================
//
// 1. podNotReady pods NEVER receive traffic (excluded from routing)
// 2. State transitions preserve breaker capacity and refCount
// 3. Active requests complete even when pod demoted to not-ready
// 4. Only atomic CAS operations for state transitions (no races)
// 5. Health checks are independent (controlled by enableQuarantine)
// 6. Quarantine/recovery states can coexist with QP authority states
// 7. Capacity based on ALL routable trackers (podReady + podRecovering when enabled)
// 8. Filtering at routing time excludes non-routable pods
//
// ============================================================================
// CAPACITY CALCULATION
// ============================================================================
//
// Capacity = sum of all routable pod trackers
// Routable states:
//   - podReady (always routable)
//   - podRecovering (only when enableQuarantine=true)
//
// Non-routable states (excluded from capacity):
//   - podNotReady (waiting for ready signal, draining, or unhealthy)
//   - podQuarantined (health check failed)
//
// This prevents capacity starvation while pods are starting up and ensures
// dynamic adjustment based on QP readiness signals and health check results.

type podTracker struct {
	id         string
	createdAt  int64
	dest       string
	b          breaker
	revisionID types.NamespacedName
	logger     *zap.SugaredLogger

	// State machine for pod health transitions
	state atomic.Uint32 // Uses podState constants
	// Reference count for in-flight requests to support graceful draining
	refCount atomic.Uint64
	// Unix timestamp when the pod should exit quarantine state (only used when enableQuarantine=true)
	quarantineEndTime atomic.Int64
	// Number of consecutive quarantine events for this pod (only used when enableQuarantine=true)
	quarantineCount atomic.Uint32

	// Queue-proxy push tracking (for trust hierarchy over K8s informer)
	// Unix timestamp of last queue-proxy push update (0 if never received)
	lastQPUpdate atomic.Int64
	// Last queue-proxy event type: "startup", "ready", "not-ready", "draining"
	// Note: atomic.Value with strings causes small allocations, but QP events are infrequent
	// (3-10 per pod lifetime) so impact is negligible. Alternative of uint32 enum adds complexity.
	lastQPState atomic.Value

	// Reason for current state transition (e.g. "qp-ready", "qp-not-ready", "qp-draining", "informer-added")
	// Used for logging/debugging to understand state changes
	// Only modified under write lock, so no atomic needed
	stateReason string

	// weight is used for LB policy implementations.
	weight atomic.Uint32
}

type podState uint32

const (
	podReady       podState = iota
	podQuarantined          // Pod failed health check (only used when enableQuarantine=true)
	podRecovering           // Pod recovering from quarantine (only used when enableQuarantine=true)
	podNotReady             // Pod not ready to receive traffic (not routable, includes draining)
)

// stateToString converts podState to string for logging/metrics
func stateToString(state podState) string {
	switch state {
	case podReady:
		return "ready"
	case podQuarantined:
		return "quarantined"
	case podRecovering:
		return "recovering"
	case podNotReady:
		return "not-ready"
	default:
		return "unknown"
	}
}

// Reference counting helper methods
func (p *podTracker) addRef() {
	p.refCount.Add(1)
}

func (p *podTracker) releaseRef() {
	// Use CAS loop to prevent TOCTOU race between Load() and Add()
	// Without CAS: two goroutines releasing when refCount=1 could both pass the zero check
	// and decrement, causing underflow
	for {
		current := p.refCount.Load()
		if current == 0 {
			// This should never happen in correct code - fail fast to catch bugs immediately
			// RefCount underflow indicates a critical bug in request lifecycle management:
			// - Reserve/release mismatch (released more than reserved)
			// - Double-release of the same request
			// - Race condition in callback handling
			p.logger.Fatalw("CRITICAL BUG: RefCount underflow - attempted to release ref with zero refcount",
				"dest", p.dest,
				"tracker-id", p.id,
				"revision", p.revisionID.String())
		}
		// Atomically decrement only if still at expected value
		if p.refCount.CompareAndSwap(current, current-1) {
			return
		}
		// CAS failed (another goroutine modified refCount), retry
	}
}

// getRefCount returns the current reference count.
// WARNING: This value can become stale immediately after reading (TOCTOU).
// For observability/logging: atomic loads are safe for concurrent reads.
// For decision-making: callers must hold revisionThrottler.mux write lock.
// The atomic load guarantees we never see a torn/invalid refCount value.
func (p *podTracker) getRefCount() uint64 {
	return p.refCount.Load()
}

func (p *podTracker) increaseWeight() {
	p.weight.Add(1)
}

func (p *podTracker) decreaseWeight() {
	if p.weight.Load() > 0 {
		p.weight.Add(^uint32(0))
	}
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

func (p *podTracker) UpdateConcurrency(c uint64) {
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
	// ONLY healthy and Recovering pods can accept new requests
	// podNotReady pods are excluded from routing until explicitly promoted to healthy
	if state != podReady && state != podRecovering {
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

// ============================================================================
// State Machine Decision Logic
// ============================================================================
//
// These functions encapsulate the rules for pod state transitions.
// Invalid transitions result in log.Fatal to catch bugs immediately.

// qpFreshnessInfo holds QP freshness data for decision making
type qpFreshnessInfo struct {
	lastSeen  int64
	age       int64
	lastEvent string
}

// getQPFreshness extracts QP freshness information from this tracker
func (p *podTracker) getQPFreshness() qpFreshnessInfo {
	lastSeen := p.lastQPUpdate.Load()
	age := time.Now().Unix() - lastSeen

	var lastEvent string
	if val := p.lastQPState.Load(); val != nil {
		if s, ok := val.(string); ok {
			lastEvent = s
		}
	}

	return qpFreshnessInfo{
		lastSeen:  lastSeen,
		age:       age,
		lastEvent: lastEvent,
	}
}

// validateTransition checks if a state transition is valid and log.Fatal if not.
// This enforces the state machine rules and catches bugs immediately.
// Self-transitions are allowed to handle duplicate informer notifications and QP events gracefully.
func (p *podTracker) validateTransition(from, to podState, context string) {
	// Define valid transitions (including self-loops for duplicate events)
	validTransitions := map[podState][]podState{
		podNotReady:    {podNotReady, podReady},
		podReady:       {podReady, podNotReady, podQuarantined},
		podQuarantined: {podQuarantined, podRecovering},
		podRecovering:  {podRecovering, podReady, podQuarantined, podNotReady},
	}

	// Check if transition is valid
	allowedTargets, exists := validTransitions[from]
	if !exists {
		// Unknown source state
		p.logger.Fatalw("INVALID STATE TRANSITION - unknown source state",
			"dest", p.dest,
			"tracker-id", p.id,
			"from", from,
			"to", to,
			"context", context)
	}

	// Check if target state is allowed
	for _, allowed := range allowedTargets {
		if allowed == to {
			return // Valid transition
		}
	}

	// Invalid transition
	p.logger.Fatalw("INVALID STATE TRANSITION",
		"dest", p.dest,
		"tracker-id", p.id,
		"from", from,
		"to", to,
		"allowed-targets", allowedTargets,
		"context", context)
}

// checkQuarantineExpiration checks if this quarantined pod should transition to recovering.
// Returns true if transition occurred.
// This is called from the hot path (filterAvailableTrackers) and must be fast.
func (p *podTracker) checkQuarantineExpiration(now int64) bool {
	_, quarantineEnabled := getFeatureGates()
	if !quarantineEnabled {
		return false
	}

	state := podState(p.state.Load())
	if state != podQuarantined {
		return false
	}

	quarantineEnd := p.quarantineEndTime.Load()
	if now >= quarantineEnd {
		// Validate transition before attempting
		p.validateTransition(podQuarantined, podRecovering, "quarantine_expiration")

		if p.state.CompareAndSwap(uint32(podQuarantined), uint32(podRecovering)) {
			// Decrement quarantine gauge (pod exiting quarantine to recovery)
			decrementQuarantineGauge(context.Background(), p)
			return true
		}
	}
	return false
}

// tryPromoteRecovering attempts to promote recovering pod to ready after successful request.
// Returns true if promotion occurred and resets quarantine count.
func (p *podTracker) tryPromoteRecovering() bool {
	currentState := podState(p.state.Load())
	if currentState != podRecovering {
		return false
	}

	// Validate transition
	p.validateTransition(podRecovering, podReady, "successful_request_recovery")

	if p.state.CompareAndSwap(uint32(podRecovering), uint32(podReady)) {
		// Reset quarantine count on successful recovery
		p.quarantineCount.Store(0)
		return true
	}

	return false
}

func newPodTracker(dest string, revisionID types.NamespacedName, b breaker, logger *zap.SugaredLogger) *podTracker {
	tracker := &podTracker{
		id:         string(uuid.NewUUID()),
		createdAt:  time.Now().UnixMicro(),
		dest:       dest,
		revisionID: revisionID,
		b:          b,
		logger:     logger,
	}
	tracker.state.Store(uint32(podNotReady))
	tracker.refCount.Store(0)
	tracker.weight.Store(0)
	tracker.lastQPUpdate.Store(0)
	tracker.lastQPState.Store("")

	return tracker
}
