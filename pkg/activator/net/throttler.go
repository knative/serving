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
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/maps"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

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
	servingconfig "knative.dev/serving/pkg/apis/config"
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

// Feature gates for activator behavior
// These are loaded from the config-features ConfigMap at runtime
var (
	// enableQPAuthority controls whether queue-proxy events trigger state changes
	// When true (default), QP events are authoritative and override K8s informer state
	// When false, activator receives but ignores QP state change events
	enableQPAuthority = true

	// enableQuarantine controls whether the health-check and quarantine system is active
	// When true (default), pods are health-checked and quarantined on failures
	// When false, no health checks or quarantine logic is used
	enableQuarantine = true

	// featureGateMutex protects feature gate access to prevent races
	featureGateMutex sync.RWMutex
)

// setFeatureGatesForTesting allows tests to override feature gates with automatic cleanup.
// Uses t.Cleanup() to restore previous state after test completes, ensuring test independence
// regardless of execution order (e.g., with -shuffle flag).
// The *testing.T parameter prevents accidental use in production code.
func setFeatureGatesForTesting(t interface{ Helper(); Cleanup(func()) }, qpAuthority, quarantine bool) {
	t.Helper()

	// Capture current state for cleanup
	featureGateMutex.Lock()
	previousQPAuthority := enableQPAuthority
	previousQuarantine := enableQuarantine
	enableQPAuthority = qpAuthority
	enableQuarantine = quarantine
	featureGateMutex.Unlock()

	// Register cleanup to restore previous state
	t.Cleanup(func() {
		featureGateMutex.Lock()
		defer featureGateMutex.Unlock()
		enableQPAuthority = previousQPAuthority
		enableQuarantine = previousQuarantine
	})
}

// setFeatureGatesForTestMain sets feature gates for TestMain without automatic cleanup.
// Use this only in TestMain where *testing.T is not available. Must manually call
// resetFeatureGatesForTesting() before os.Exit().
func setFeatureGatesForTestMain(qpAuthority, quarantine bool) {
	featureGateMutex.Lock()
	defer featureGateMutex.Unlock()
	enableQPAuthority = qpAuthority
	enableQuarantine = quarantine
}

// resetFeatureGatesForTesting resets feature gates to defaults.
// Only use in TestMain after calling setFeatureGatesForTestMain.
func resetFeatureGatesForTesting() {
	featureGateMutex.Lock()
	defer featureGateMutex.Unlock()
	enableQPAuthority = true
	enableQuarantine = true
}

// Prometheus metrics for monitoring QP authoritative state system
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

	// qpAuthorityOverrides tracks when QP data overrides K8s informer
	// Labels: action (ignored_promotion or ignored_demotion), reason
	qpAuthorityOverrides = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pod_qp_authority_overrides_total",
			Help: "Number of times QP state overrode K8s informer state",
		},
		[]string{"action", "reason"},
	)
)

func newPodTracker(dest string, revisionID types.NamespacedName, b breaker) *podTracker {
	tracker := &podTracker{
		id:         string(uuid.NewUUID()),
		createdAt:  time.Now().UnixMicro(),
		dest:       dest,
		revisionID: revisionID,
		b:          b,
	}
	tracker.state.Store(uint32(podNotReady))
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
	podReady       podState = iota
	podDraining             // Graceful shutdown - only from QP "draining" event when enableQPAuthority=true
	podQuarantined          // Pod failed health check (only used when enableQuarantine=true)
	podRecovering           // Pod recovering from quarantine (only used when enableQuarantine=true)
	podRemoved
	podNotReady // Pod not ready to receive traffic (not routable)
)

// Pod State Machine Documentation
//
// The activator implements a 6-state pod state machine with two independent feature gates:
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
//              │                           │                                      │
//              │                           │                                      │
//              │                    QP "not-ready"                         after backoff
//              │                           │                                      ↓
//              │                           ↓                                podRecovering
//              │                      podNotReady ←──health check fails────────┘
//              │                           │                                      │
//              └─────QP "draining"─────→   │   ←────QP "draining"────────────────┘
//                                          ↓
//                                    podDraining
//                                          │
//                                   refCount → 0
//                                          ↓
//                                     podRemoved
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
// States used: All 6 states
// Initial state: podNotReady (promoted by QP "ready" event)
// State transitions:
//   - NEW POD → podNotReady (waiting for QP ready signal)
//   - QP "ready" → podReady (pod now routable)
//   - QP "not-ready" → podNotReady (pod stops receiving traffic but preserves refCount)
//   - Health check failure → podQuarantined (temporary quarantine)
//   - After backoff → podRecovering (re-check health)
//   - Health check success → podReady (back to normal)
//   - QP "draining" → podDraining (graceful shutdown)
//   - refCount → 0 → podRemoved (cleanup)
//
// Mode 2: QP AUTHORITY ONLY (QP Authority ON, Quarantine OFF)
// ────────────────────────────────────────────────────────────
// Queue-proxy events are authoritative, no health checks.
//
// States used: podReady, podDraining, podNotReady, podRemoved
// Initial state: podNotReady (promoted by QP "ready" event)
// State transitions:
//   - NEW POD → podNotReady (waiting for QP ready signal)
//   - QP "ready" → podReady
//   - QP "not-ready" → podNotReady
//   - QP "draining" → podDraining
//   - refCount → 0 → podRemoved
//
// Mode 3: QUARANTINE ONLY (QP Authority OFF, Quarantine ON)
// ──────────────────────────────────────────────────────────
// K8s informer is sole authority, health checks run and can quarantine.
//
// States used: podReady, podQuarantined, podRecovering, podNotReady, podRemoved
// Initial state: podReady (K8s informer controls promotion)
// State transitions:
//   - NEW POD → podReady (trust K8s immediately)
//   - Health check failure → podQuarantined
//   - After backoff → podRecovering
//   - Health check success → podReady
//   - K8s "not ready" → podNotReady
//   - K8s removal → podRemoved
//
// Mode 4: MINIMAL (Both OFF)
// ───────────────────────────
// K8s informer only, no health checks, no QP authority.
//
// States used: podReady, podNotReady, podRemoved
// Initial state: podReady (K8s informer controls everything)
// State transitions:
//   - NEW POD → podReady (trust K8s)
//   - K8s "not ready" → podNotReady
//   - K8s removal → podRemoved
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
//     * Health check failure (when enableQuarantine=true) → podQuarantined
//     * QP "draining" event (when enableQPAuthority=true) → podDraining
//     * K8s informer removal → podDraining (if QP silent) or podNotReady
//
// podDraining (1): Pod is shutting down gracefully
//   - Routable: NO (excluded from routing pool)
//   - Can receive new requests: NO
//   - Active requests preserved: YES (refCount tracked)
//   - Entry conditions:
//     * QP "draining" event (requires enableQPAuthority=true)
//     * K8s informer removal (when QP data stale)
//   - Exit conditions:
//     * refCount → 0 → podRemoved (normal graceful shutdown)
//     * Timeout (maxDrainingDuration) → podRemoved (force cleanup)
//     * Re-added to endpoints → podNotReady (treat as new pod)
//
// podQuarantined (2): Pod failed health check and is temporarily quarantined
//   - Routable: NO (only used when enableQuarantine=true)
//   - Can receive new requests: NO
//   - Active requests preserved: YES (existing requests complete)
//   - Entry conditions: Health check failure from podReady or podRecovering
//   - Exit conditions:
//     * After backoff period → podRecovering (re-check health)
//     * K8s informer removal → podRemoved
//
// podRecovering (3): Pod recovering from quarantine, being re-tested
//   - Routable: YES (only used when enableQuarantine=true)
//   - Can receive new requests: YES (limited during recovery)
//   - Active requests preserved: YES
//   - Entry conditions: After quarantine backoff expires
//   - Exit conditions:
//     * Health check success → podReady (recovered)
//     * Health check failure → podQuarantined (back to quarantine)
//     * K8s informer removal → podDraining
//
// podRemoved (4): Pod completely removed from tracker (terminal state)
//   - Routable: NO
//   - Can receive new requests: NO
//   - Active requests preserved: NO
//   - Entry conditions:
//     * From podDraining when refCount → 0
//     * Force cleanup after maxDrainingDuration
//   - Exit conditions:
//     * Re-added to endpoints → podNotReady (create new tracker)
//
// podNotReady (5): Pod exists but not ready to receive traffic
//   - Routable: NO (excluded from routing pool)
//   - Can receive new requests: NO
//   - Active requests preserved: YES (refCount from previous podReady state)
//   - Entry conditions:
//     * New pod creation (initial state when enableQPAuthority=true)
//     * QP "not-ready" event (demotion from podReady)
//     * QP "startup" event (explicit not-ready signal)
//     * K8s informer "not ready" (when QP not authoritative)
//   - Exit conditions:
//     * QP "ready" event → podReady (promotion when enableQPAuthority=true)
//     * K8s informer "healthy" → podReady (when QP data stale or disabled)
//     * QP "draining" event → podDraining (crash before ready)
//     * K8s informer removal → podRemoved (cleanup)
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
//   - "draining": Transitions podReady → podDraining (graceful shutdown)
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
//   - podNotReady (waiting for ready signal)
//   - podQuarantined (health check failed)
//   - podDraining (shutting down)
//   - podRemoved (terminal state)
//
// This prevents capacity starvation while pods are starting up and ensures
// dynamic adjustment based on QP readiness signals and health check results.

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
	// Unix timestamp when the pod should exit quarantine state (only used when enableQuarantine=true)
	quarantineEndTime atomic.Int64
	// Number of consecutive quarantine events for this pod (only used when enableQuarantine=true)
	quarantineCount atomic.Uint32

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
	if p.state.CompareAndSwap(uint32(podReady), uint32(podDraining)) {
		p.drainingStartTime.Store(time.Now().Unix())
		return true
	}
	// When quarantine is enabled, also allow draining from podRecovering state
	if enableQuarantine && p.state.CompareAndSwap(uint32(podRecovering), uint32(podDraining)) {
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

// Quarantine system functions (only used when enableQuarantine=true)

// transitionOutOfQuarantine ensures we decrement quarantine gauge exactly once
// and optionally set a new state. Returns true if a decrement happened.
func transitionOutOfQuarantine(ctx context.Context, p *podTracker, newState podState) bool {
	if !enableQuarantine || p == nil {
		return false
	}
	// Use CAS to atomically transition from quarantined to new state
	if p.state.CompareAndSwap(uint32(podQuarantined), uint32(newState)) {
		// Successfully transitioned out of quarantine
		handler.RecordPodQuarantineChange(ctx, -1)
		handler.RecordPodQuarantineExit(ctx)
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

// quarantineBackoffSeconds returns backoff seconds for a given consecutive quarantine count.
// For pending pods (never been healthy): 0s, 1s, 1s, 2s, 5s (be aggressive in retrying new pods)
// For established pods (was healthy): 1s, 2s, 5s, 10s, 20s (more conservative for known-good pods)
func quarantineBackoffSeconds(count uint32) uint32 {
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

// podReadyCheckFunc holds the function used for health checking pods
var podReadyCheckFunc atomic.Value

// podReadyCheckClient is reused across all health checks to avoid allocating a new client per call
var podReadyCheckClient = &http.Client{
	Timeout: podReadyCheckTimeout,
	Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout: podReadyCheckTimeout,
		}).DialContext,
	},
}

func init() {
	podReadyCheckFunc.Store(podReadyCheck)
}

// podReadyCheck performs HTTP health check against queue-proxy
func podReadyCheck(dest string, expectedRevision types.NamespacedName) bool {
	ctx, cancel := context.WithTimeout(context.Background(), podReadyCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+dest+"/", nil)
	if err != nil {
		return false
	}

	req.Header.Set("User-Agent", "kube-probe/activator")
	req.Header.Set(netheader.ProbeKey, queue.Name)

	resp, err := podReadyCheckClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}

	respRevName := resp.Header.Get("X-Knative-Revision-Name")
	respRevNamespace := resp.Header.Get("X-Knative-Revision-Namespace")

	// Backwards compatibility: If headers are not present (old queue-proxy), skip validation
	if respRevName == "" && respRevNamespace == "" {
		return true
	}

	// If headers ARE present, validate they match expected revision
	if respRevName != expectedRevision.Name || respRevNamespace != expectedRevision.Namespace {
		if logger := logging.FromContext(context.Background()); logger != nil {
			logger.Errorw("Health check response from wrong revision - IP reuse detected!",
				"dest", dest,
				"expected-revision", expectedRevision.String(),
				"response-revision", types.NamespacedName{Name: respRevName, Namespace: respRevNamespace}.String())
		}
		return false
	}

	return true
}

// tcpPingCheck invokes the pod ready check function
func tcpPingCheck(dest string, expectedRevision types.NamespacedName) bool {
	featureGateMutex.RLock()
	quarantineEnabled := enableQuarantine
	featureGateMutex.RUnlock()

	if !quarantineEnabled {
		return true // Skip health checks when quarantine is disabled
	}
	fn := podReadyCheckFunc.Load().(func(string, types.NamespacedName) bool)
	return fn(dest, expectedRevision)
}

// filterAvailableTrackers returns only healthy pods ready to serve traffic
// podNotReady pods are excluded until explicitly promoted to healthy
// When enableQuarantine=true, also filters out quarantined/recovering pods
func (rt *revisionThrottler) filterAvailableTrackers(trackers []*podTracker) []*podTracker {
	available := make([]*podTracker, 0, len(trackers))

	now := time.Now().Unix()
	for _, tracker := range trackers {
		if tracker == nil {
			continue
		}
		state := podState(tracker.state.Load())

		// When quarantine is enabled, handle quarantine/recovering states
		if enableQuarantine {
			switch state {
			case podQuarantined:
				// Check if quarantine period has expired
				quarantineEnd := tracker.quarantineEndTime.Load()
				if now >= quarantineEnd {
					// Transition to recovering state (probation period)
					if tracker.state.CompareAndSwap(uint32(podQuarantined), uint32(podRecovering)) {
						rt.logger.Debugw("Pod exiting quarantine, entering recovery",
							"dest", tracker.dest,
							"quarantine-count", tracker.quarantineCount.Load())
						// Decrement quarantine gauge
						transitionOutOfQuarantine(context.Background(), tracker, podRecovering)
					}
				}
				continue // Skip quarantined pods
			case podRecovering:
				// Allow recovering pods to receive traffic (probation period)
				available = append(available, tracker)
			case podReady:
				available = append(available, tracker)
			default:
				// Skip pending, draining, removed
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

			// Health check logic (only when enableQuarantine=true)
			// CRITICAL: Only health check routable pods (podReady, podRecovering)
			// podNotReady pods are NOT routable so cannot be health checked
			if enableQuarantine && !isClusterIP {
				currentState := podState(tracker.state.Load())

				// Skip health checks for podNotReady (not routable)
				if currentState != podNotReady {
					// Track health check duration
					healthCheckStart := time.Now()
					healthCheckPassed := tcpPingCheck(tracker.dest, tracker.revisionID)
					healthCheckMs := float64(time.Since(healthCheckStart).Milliseconds())

					if healthCheckMs > 1000 { // Log if health check took >1s
						rt.logger.Warnw("Slow health check",
							"x-request-id", xRequestId,
							"dest", tracker.dest,
							"state", currentState,
							"health-check-ms", healthCheckMs,
							"passed", healthCheckPassed)
					}

					if !healthCheckPassed {
						rt.logger.Errorw("pod ready check failed; quarantine",
							"x-request-id", xRequestId,
							"dest", tracker.dest,
							"revision", rt.revID.String(),
							"tracker-id", tracker.id,
							"previous-state", currentState,
							"reenqueue-count", reenqueueCount)

						// Try to quarantine this tracker using CAS to avoid races
						// We can transition from podReady or podRecovering to podQuarantined
						wasQuarantined := false
						for !wasQuarantined {
							prevState := podState(tracker.state.Load())
							if prevState == podQuarantined {
								// Already quarantined by another goroutine
								break
							}
							// Only quarantine from routable states
							if prevState != podReady && prevState != podRecovering {
								break
							}
							if tracker.state.CompareAndSwap(uint32(prevState), uint32(podQuarantined)) {
								wasQuarantined = true
								// Only update metrics if we actually performed the quarantine
								// Increment consecutive quarantine count
								count := tracker.quarantineCount.Add(1)
								// Determine backoff duration
								backoff := quarantineBackoffSeconds(count)
								tracker.quarantineEndTime.Store(time.Now().Unix() + int64(backoff))
								// Record metrics
								handler.RecordPodQuarantineChange(ctx, 1)
								handler.RecordPodQuarantineEntry(ctx)
								handler.RecordTCPPingFailureEvent(ctx)
							}
						}
						// Re-queue the request to try another backend
						reenqueue = true
						return
					}
				}
			}

			// Time the actual proxy call
			proxyCallStart := time.Now()
			ret = function(tracker.dest, isClusterIP)
			proxyDurationMs = float64(time.Since(proxyCallStart).Milliseconds())

			// When enableQuarantine=true, handle post-request state transitions
			if enableQuarantine && ret == nil && !isClusterIP {
				// Request succeeded - if pod was recovering, promote back to healthy
				currentState := podState(tracker.state.Load())
				if currentState == podRecovering {
					if tracker.state.CompareAndSwap(uint32(podRecovering), uint32(podReady)) {
						// Reset quarantine count on successful recovery
						tracker.quarantineCount.Store(0)
						rt.logger.Infow("Pod recovered from quarantine, promoting to healthy",
							"x-request-id", xRequestId,
							"dest", tracker.dest)
					}
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

				// Check QP freshness to determine if we should trust informer (only when QP authority enabled)
				var lastQPSeen int64
				var qpAge int64
				var lastQPEvent string

				if enableQPAuthority {
					lastQPSeen = tracker.lastQPUpdate.Load()
					qpAge = time.Now().Unix() - lastQPSeen
					if val := tracker.lastQPState.Load(); val != nil {
						if s, ok := val.(string); ok {
							lastQPEvent = s
						}
					}
				}

				switch currentState {
				case podDraining, podRemoved:
					// Pod was being removed but is back in healthy endpoint list (e.g., rolling update rollback)
					// Use QP freshness to decide state - if we missed QP events, trust K8s informer
					if enableQPAuthority {
						// Only set to notReady if QP recently said "not-ready"
						// Otherwise trust K8s (QP data stale or already confirmed ready)
						if lastQPEvent == "not-ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
							// QP recently said not-ready - wait for ready event
							tracker.state.Store(uint32(podNotReady))
							rt.logger.Infow("Pod returning from drain/removal, waiting for QP ready (QP recently not-ready)",
								"dest", d, "qp-age-sec", qpAge)
						} else {
							// QP data stale/missing OR QP said ready - trust K8s informer
							tracker.state.Store(uint32(podReady))
							rt.logger.Infow("Pod returning from drain/removal, trusting K8s (QP stale or confirmed ready)",
								"dest", d, "qp-age-sec", qpAge, "qp-last-event", lastQPEvent)
						}
					} else {
						tracker.state.Store(uint32(podReady))
					}
					tracker.drainingStartTime.Store(0)
				case podReady:
					// Already healthy, nothing to do

				case podRecovering, podQuarantined:
					// Quarantine states, nothing to do
				case podNotReady:
					// K8s says healthy, pod is pending
					if enableQPAuthority {
						// Only promote if QP hasn't recently said "not-ready"
						if lastQPEvent == "not-ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
							qpAuthorityOverrides.WithLabelValues("ignored_promotion", "qp_recently_not_ready").Inc()
							rt.logger.Debugw("Ignoring K8s healthy - QP recently said not-ready",
								"dest", d,
								"qp-age-sec", qpAge,
								"qp-last-event", lastQPEvent)
							// Don't promote - trust fresh QP data
						} else if tracker.state.CompareAndSwap(uint32(podNotReady), uint32(podReady)) {
							// Safe to promote (QP hasn't objected recently, or QP data is stale)
							podStateTransitions.WithLabelValues("not-ready", "ready", "k8s_informer").Inc()
							rt.logger.Infow("K8s informer promoted not-ready pod to healthy",
								"dest", d,
								"qp-age-sec", qpAge)
						}
					} else {
						// Promote immediately (no QP to check)
						tracker.state.CompareAndSwap(uint32(podNotReady), uint32(podReady))
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

			// Check QP freshness - if QP recently said "ready", K8s might be stale (only when QP authority enabled)
			var lastQPSeen int64
			var qpAge int64
			var lastQPEvent string

			if enableQPAuthority {
				lastQPSeen = tracker.lastQPUpdate.Load()
				qpAge = now - lastQPSeen
				if val := tracker.lastQPState.Load(); val != nil {
					if s, ok := val.(string); ok {
						lastQPEvent = s
					}
				}

				// CRITICAL: If QP recently said "ready", don't drain (informer is stale)
				if currentState == podReady && lastQPEvent == "ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
					qpAuthorityOverrides.WithLabelValues("ignored_drain", "qp_recently_ready").Inc()
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
			}

			switch currentState {
			case podReady, podRecovering:
				// When QP authority is enabled, use proper draining (graceful shutdown)
				// When QP authority is disabled, just mark as not-ready (K8s informer says pod is going away)
				if enableQPAuthority {
					if tracker.tryDrain() {
						fromState := "ready"
						if currentState == podRecovering {
							fromState = "recovering"
						}
						podStateTransitions.WithLabelValues(fromState, "draining", "k8s_informer").Inc()
						rt.logger.Debugf("Pod %s transitioning to draining state, refCount=%d", d, tracker.getRefCount())
						if tracker.getRefCount() == 0 {
							tracker.state.Store(uint32(podRemoved))
							delete(rt.podTrackers, d)
							rt.logger.Debugf("Pod %s removed immediately (no active requests)", d)
						}
					}
				} else {
					// QP authority disabled - transition to not-ready instead of draining
					if tracker.state.CompareAndSwap(uint32(currentState), uint32(podNotReady)) {
						podStateTransitions.WithLabelValues("ready", "not-ready", "k8s_informer").Inc()
						rt.logger.Debugf("Pod %s transitioning to not-ready (K8s removing)", d)
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
				// When quarantine is enabled, remove quarantined pods cleanly
				if enableQuarantine {
					transitionOutOfQuarantine(context.Background(), tracker, podRemoved)
					delete(rt.podTrackers, d)
					rt.logger.Infow("Pod removed while in quarantine",
						"dest", d,
						"revision", rt.revID.String(),
						"tracker-id", tracker.id)
				} else {
					// Quarantine not enabled - treat as normal removal
					tracker.state.Store(uint32(podRemoved))
					delete(rt.podTrackers, d)
					rt.logger.Debugf("Pod %s removed while in unexpected quarantined state", d)
				}
			case podNotReady:
				// Pod being removed while not ready
				tracker.state.Store(uint32(podRemoved))
				delete(rt.podTrackers, d)
				rt.logger.Debugf("Pod %s removed while not-ready", d)
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
				case podNotReady:
					// Pod is pending - let QP or informer promote it
					rt.logger.Debugf("Pod %s in pending state, waiting for promotion", newDest)
				case podDraining, podRemoved:
					// Pod was removed but is now back in the endpoint list (e.g., controller restart)
					// Reset to pending - QP will promote to healthy when ready
					tracker.state.Store(uint32(podNotReady))
					tracker.drainingStartTime.Store(0)
					rt.logger.Debugw("Re-adding previously draining/removed pod as pending",
						"dest", newDest,
						"previousState", currentState)
				case podReady:
					// Already healthy, nothing to do
				}
			}
		}
		// Build healthyDests from ALL dests in update.Dests (both new and existing)
		// This ensures new pods get promoted from pending to healthy
		healthyDests := make([]string, 0, len(update.Dests))
		for newDest := range update.Dests {
			healthyDests = append(healthyDests, newDest)
		}

		// Build drainingDests from pods that are no longer in the update
		drainingDests := make([]string, 0)
		for _, d := range currentDests {
			if _, ok := newDestsSet[d]; !ok {
				// Pod is no longer in the active set, needs draining/removal
				drainingDests = append(drainingDests, d)
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
// - "startup": creates new tracker (podNotReady when enableQPAuthority=true, podReady when false)
// - "ready": promotes podNotReady → podReady - only when enableQPAuthority=true
// - "not-ready": demotes podReady → podNotReady - only when enableQPAuthority=true
// - "draining": transitions podReady → podDraining - only when enableQPAuthority=true
// When enableQPAuthority=false, QP events are received and logged but don't trigger state changes
//
// PERFORMANCE: Lock hold time minimized to reduce contention under high pod registration load.
// Logging and metrics recording happen outside critical section.
func (rt *revisionThrottler) addPodIncremental(podIP string, eventType string, logger *zap.SugaredLogger) {
	// Variables to capture state for logging/metrics outside lock
	var (
		shouldLog          bool
		logLevel           string // "info", "warn", "debug"
		logMessage         string
		logPodIP           string
		logRevision        string
		logCurrentState    podState
		logRefCount        uint64
		shouldRecordMetric bool
		metricFrom         string
		metricTo           string
	)

	rt.mux.Lock()

	existing, exists := rt.podTrackers[podIP]

	if exists {
		// Always update QP freshness tracking (atomic ops, fast)
		existing.lastQPUpdate.Store(time.Now().Unix())
		existing.lastQPState.Store(eventType)

		// When QP authority is disabled, just log but don't change state
		if !enableQPAuthority {
			rt.mux.Unlock()
			logger.Debugw("Received QP event (QP authority disabled, no state change)",
				"revision", rt.revID.String(),
				"pod-ip", podIP,
				"event-type", eventType,
				"current-state", podState(existing.state.Load()))
			return
		}

		currentState := podState(existing.state.Load())

		// Perform state transitions - NO logging inside lock (only when enableQPAuthority=true)
		switch eventType {
		case "startup":
			// Duplicate startup - capture for logging
			shouldLog = true
			logLevel = "debug"
			logMessage = "Duplicate startup event (pod already tracked)"
			logPodIP = podIP
			logRevision = rt.revID.String()
			logCurrentState = currentState

		case "ready":
			// QP says ready - promote to ready immediately
			switch currentState {
			case podNotReady:
				if existing.state.CompareAndSwap(uint32(podNotReady), uint32(podReady)) {
					// Transition succeeded - capture for metric and log
					shouldRecordMetric = true
					metricFrom = "not-ready"
					metricTo = "ready"
					shouldLog = true
					logLevel = "info"
					logMessage = "QP promoted not-ready pod to ready"
					logPodIP = podIP
					logRevision = rt.revID.String()
				}
			case podDraining, podRemoved:
				// Stale ready event on draining/removed pod - log and ignore
				shouldLog = true
				logLevel = "warn"
				logMessage = "Ignoring stale ready event on draining/removed pod"
				logPodIP = podIP
				logRevision = rt.revID.String()
				logCurrentState = currentState
			default:
				// Already ready - duplicate event
				shouldLog = true
				logLevel = "debug"
				logMessage = "Duplicate ready event (pod already ready)"
				logPodIP = podIP
				logRevision = rt.revID.String()
				logCurrentState = currentState
			}

		case "not-ready":
			// QP says not-ready - demote to not-ready state
			// CRITICAL: Preserves breaker and refCount - just stops NEW traffic
			switch currentState {
			case podReady:
				refCount := existing.refCount.Load() // Capture before CAS
				if existing.state.CompareAndSwap(uint32(podReady), uint32(podNotReady)) {
					// Transition succeeded
					shouldRecordMetric = true
					metricFrom = "ready"
					metricTo = "not-ready"
					shouldLog = true
					logLevel = "warn"
					logMessage = "QP demoted pod to not-ready (readiness probe failed)"
					logPodIP = podIP
					logRevision = rt.revID.String()
					logRefCount = refCount
				}
			case podDraining, podRemoved:
				// Stale not-ready event on draining/removed pod - log and ignore
				shouldLog = true
				logLevel = "warn"
				logMessage = "Ignoring stale not-ready event on draining/removed pod"
				logPodIP = podIP
				logRevision = rt.revID.String()
				logCurrentState = currentState
			case podNotReady:
				// Not-ready on not-ready pod - duplicate event
				shouldLog = true
				logLevel = "debug"
				logMessage = "not-ready event on not-ready pod (duplicate)"
				logPodIP = podIP
				logRevision = rt.revID.String()
			}

		case "draining":
			// QP says draining - transition to draining state
			refCount := existing.refCount.Load() // Capture before state change

			if existing.tryDrain() {
				// Transitioned ready → draining
				shouldRecordMetric = true
				metricFrom = "ready"
				metricTo = "draining"
				shouldLog = true
				logLevel = "info"
				logMessage = "QP initiated pod draining"
				logPodIP = podIP
				logRevision = rt.revID.String()
				logRefCount = refCount
			} else if currentState == podNotReady {
				// Pod draining before becoming ready (crash during startup)
				// Allow pending → draining transition
				if existing.state.CompareAndSwap(uint32(podNotReady), uint32(podDraining)) {
					existing.drainingStartTime.Store(time.Now().Unix())
					shouldRecordMetric = true
					metricFrom = "not-ready"
					metricTo = "draining"
					shouldLog = true
					logLevel = "warn"
					logMessage = "Pod draining before ready - crashed during startup"
					logPodIP = podIP
					logRevision = rt.revID.String()
				}
			} else if currentState == podDraining {
				// Already draining - duplicate event, log and ignore
				shouldLog = true
				logLevel = "debug"
				logMessage = "Duplicate draining event (pod already draining)"
				logPodIP = podIP
				logRevision = rt.revID.String()
			} else if currentState == podRemoved {
				// Stale draining event on removed pod - log and ignore
				shouldLog = true
				logLevel = "debug"
				logMessage = "Ignoring stale draining event on removed pod"
				logPodIP = podIP
				logRevision = rt.revID.String()
			}
		}

		rt.mux.Unlock()

		// Log and record metrics OUTSIDE lock to minimize contention
		if shouldRecordMetric {
			podStateTransitions.WithLabelValues(metricFrom, metricTo, "qp_push").Inc()
		}

		if shouldLog {
			switch logLevel {
			case "info":
				if logRefCount > 0 {
					logger.Infow(logMessage,
						"revision", logRevision,
						"pod-ip", logPodIP,
						"active-requests", logRefCount)
				} else {
					logger.Infow(logMessage,
						"revision", logRevision,
						"pod-ip", logPodIP)
				}
			case "warn":
				logger.Warnw(logMessage,
					"revision", logRevision,
					"pod-ip", logPodIP,
					"active-requests", logRefCount)
			case "debug":
				if logCurrentState != 0 {
					logger.Debugw(logMessage,
						"revision", logRevision,
						"pod-ip", logPodIP,
						"current-state", logCurrentState)
				} else {
					logger.Debugw(logMessage,
						"revision", logRevision,
						"pod-ip", logPodIP)
				}
			}
		}

		return
	}

	// Pod doesn't exist - create new tracker OUTSIDE lock to minimize contention
	//
	// RACE WINDOW DOCUMENTATION:
	// ===========================
	// We release the lock here (line 2076) while creating the new tracker, then re-acquire
	// it to insert into the map (line 2108). This creates a race window where multiple
	// concurrent registrations for the same pod IP could all create trackers.
	//
	// Race scenario:
	//   T0: Goroutine A checks map, pod not found, releases lock
	//   T1: Goroutine B checks map, pod not found, releases lock
	//   T2: Goroutine A creates tracker, re-acquires lock, inserts pod
	//   T3: Goroutine B creates tracker, re-acquires lock, finds existing pod
	//
	// Impact: Wasted allocations (breaker creation is expensive) under high pod churn
	// Correctness: Race is handled correctly - second goroutine discards its tracker
	//              and updates the existing one's QP data (lines 2111-2119)
	//
	// Trade-off analysis:
	//   PRO: Minimizes lock hold time - critical for high-throughput registration
	//   PRO: Race is rare in practice (requires simultaneous registration of same pod)
	//   CON: Wasted allocations when race occurs (breaker + tracker creation)
	//
	// Alternative approaches considered:
	//   1. Hold lock during tracker creation:
	//      - Eliminates race but increases lock contention significantly
	//      - Could block other pod registrations during breaker creation
	//      - Not acceptable for production at scale
	//
	//   2. sync.Once per pod IP:
	//      - Prevents duplicate allocations
	//      - Requires maintaining a separate map[string]*sync.Once
	//      - Memory overhead and cleanup complexity
	//      - Potential optimization if profiling shows this is a bottleneck
	//
	//   3. Double-checked locking with atomic:
	//      - Check map without lock, create if needed, check again with lock
	//      - Reduces but doesn't eliminate race window
	//      - Added complexity for minimal gain
	//
	// Verdict: Current implementation is acceptable as-is. The race is handled correctly
	//          and only causes wasted allocations in rare cases. If profiling reveals this
	//          as a bottleneck (>1% of pod registrations hitting the race), consider
	//          sync.Once optimization. For now, correctness and lock minimization take
	//          priority over avoiding rare allocation waste.
	//
	cc := int(rt.containerConcurrency.Load())

	rt.mux.Unlock() // Release lock while creating tracker (expensive)

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

	// Set initial state based on enableQPAuthority and event type
	// When QP authority is enabled: start as podNotReady, wait for "ready" event
	// When QP authority is disabled: start as podReady (old behavior, health checks will validate)
	if enableQPAuthority {
		if eventType == "ready" {
			tracker.state.Store(uint32(podReady))
		} else {
			tracker.state.Store(uint32(podNotReady))
		}
	} else {
		// QP authority disabled - start as ready, health checks will handle validation
		tracker.state.Store(uint32(podReady))
	}

	// Initialize QP tracking
	tracker.lastQPUpdate.Store(time.Now().Unix())
	tracker.lastQPState.Store(eventType)

	// Re-acquire lock to add tracker to map
	rt.mux.Lock()

	// Check if another goroutine added this pod while we were creating tracker (race check)
	if existing, exists := rt.podTrackers[podIP]; exists {
		// Race detected: another registration beat us to it during the unlock window
		// Update the existing tracker's QP data and discard our newly created tracker
		// This is correct behavior - the first tracker wins, we just update its QP state
		existing.lastQPUpdate.Store(time.Now().Unix())
		existing.lastQPState.Store(eventType)

		rt.mux.Unlock()

		logger.Debugw("Pod added by concurrent registration, updated QP tracking",
			"revision", rt.revID.String(),
			"pod-ip", podIP,
			"event-type", eventType)
		return
	}

	// Add our tracker to the map
	rt.podTrackers[podIP] = tracker
	podCount := len(rt.podTrackers)

	rt.mux.Unlock()

	// Log and trigger capacity update OUTSIDE lock
	logger.Infow("Discovered new pod via push-based registration",
		"revision", rt.revID.String(),
		"pod-ip", podIP,
		"event-type", eventType,
		"initial-state", podState(tracker.state.Load()),
		"qp-authority-enabled", enableQPAuthority,
		"total-trackers-now", podCount)

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

	// Start background cleanup goroutine for stale podNotReady trackers
	go t.cleanupStalePodTrackers(ctx)

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
// eventType should be "startup" (creates podNotReady tracker) or "ready" (promotes to podReady).
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

// UpdateFeatureGatesFromConfigMap returns a ConfigMap watcher function that updates
// the activator feature gates (enableQPAuthority and enableQuarantine) based on the
// config-features ConfigMap.
func UpdateFeatureGatesFromConfigMap(logger *zap.SugaredLogger) func(configMap *corev1.ConfigMap) {
	return func(configMap *corev1.ConfigMap) {
		if configMap == nil {
			logger.Warnw("Received nil ConfigMap for feature gates, using defaults")
			return
		}

		// Parse the features config from the ConfigMap
		features, err := servingconfig.NewFeaturesConfigFromConfigMap(configMap)
		if err != nil {
			logger.Errorw("Failed to parse features config, using existing feature gate values",
				zap.Error(err))
			return
		}

		// Update feature gates based on ConfigMap values
		featureGateMutex.Lock()
		defer featureGateMutex.Unlock()

		// Update QP Authority feature gate
		previousQPAuthority := enableQPAuthority
		enableQPAuthority = features.QueueProxyPodAuthority == servingconfig.Enabled
		if previousQPAuthority != enableQPAuthority {
			logger.Infow("Queue-proxy pod authority feature gate changed",
				"previous", previousQPAuthority,
				"new", enableQPAuthority)
		}

		// Update Quarantine feature gate
		previousQuarantine := enableQuarantine
		enableQuarantine = features.ActivatorPodQuarantine == servingconfig.Enabled
		if previousQuarantine != enableQuarantine {
			logger.Infow("Activator pod quarantine feature gate changed",
				"previous", previousQuarantine,
				"new", enableQuarantine)
		}
	}
}

// cleanupStalePodTrackers periodically removes stale podNotReady trackers with zero refCount
// to prevent memory leaks when pods crash before sending ready/draining events.
//
// Cleanup criteria:
// - Pod in podNotReady state
// - Zero active requests (refCount == 0)
// - No activity for 10+ minutes (prevents premature cleanup during slow startups)
//
// This prevents unbounded growth of the podTrackers map when:
// - Pods crash after "startup" but before "ready" event
// - QP never sends "draining" event due to crash
// - K8s informer update is delayed or missed
func (t *Throttler) cleanupStalePodTrackers(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.logger.Info("Stopping pod tracker cleanup goroutine")
			return
		case <-ticker.C:
			t.cleanupStaleTrackersOnce()
		}
	}
}

// cleanupStaleTrackersOnce performs one pass of stale tracker cleanup across all revisions
func (t *Throttler) cleanupStaleTrackersOnce() {
	t.revisionThrottlersMutex.RLock()
	revisions := make([]*revisionThrottler, 0, len(t.revisionThrottlers))
	for _, rt := range t.revisionThrottlers {
		revisions = append(revisions, rt)
	}
	t.revisionThrottlersMutex.RUnlock()

	now := time.Now().Unix()
	const staleThreshold = 600 // 10 minutes in seconds

	for _, rt := range revisions {
		rt.mux.Lock()
		for ip, tracker := range rt.podTrackers {
			state := podState(tracker.state.Load())
			refCount := tracker.refCount.Load()
			createdAt := tracker.createdAt

			// Cleanup podNotReady with zero refCount that are stale
			if state == podNotReady && refCount == 0 {
				age := now - createdAt/1e6 // createdAt is in microseconds
				if age > staleThreshold {
					delete(rt.podTrackers, ip)
					rt.logger.Infow("Cleaned up stale podNotReady tracker",
						"revision", rt.revID.String(),
						"pod-ip", ip,
						"age-seconds", age,
						"ref-count", refCount)
				}
			}
		}
		rt.mux.Unlock()
	}
}
