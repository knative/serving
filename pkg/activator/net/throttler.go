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
	"time"

	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	pkgnet "knative.dev/networking/pkg/apis/networking"
	netcfg "knative.dev/networking/pkg/config"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/reconciler"
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

	// QPFreshnessReadyWindow - Queue-proxy "ready" events trusted for this duration
	// Longer window prevents premature draining due to slow K8s updates in degraded clusters
	QPFreshnessReadyWindow = 60 * time.Second

	// QPFreshnessNotReadyWindow - Queue-proxy "not-ready" events trusted for this duration
	// Shorter window allows faster failure detection when pod health degrades
	QPFreshnessNotReadyWindow = 30 * time.Second

	// QPStalenessThreshold - Queue-proxy older than this, trust K8s instead
	// If QP has been silent this long, it's likely dead and informer is authoritative
	QPStalenessThreshold = 60 * time.Second

	// PodNotReadyStaleThreshold - Duration after which podNotReady trackers with zero refCount are cleaned up
	// This prevents memory leaks from pods that never became ready or got stuck during startup
	PodNotReadyStaleThreshold = 10 * time.Minute

	// stateUpdateQueueSize - Buffer size for the state update channel
	// Supports up to 500 pods registering simultaneously during rapid scale-up
	// Multiple concurrent handlers (typically 10-20) each sending multiple events
	// Burst absorption during K8s informer bulk updates (can update 100+ endpoints at once)
	// Empirically tested: handles 100 concurrent goroutines without blocking
	stateUpdateQueueSize = 500

	// workerShutdownTimeout - Maximum time to wait for worker shutdown
	// Allows graceful draining of pending requests before forcing exit
	workerShutdownTimeout = 5 * time.Second

	// Proxy queue time thresholds for latency monitoring
	// These thresholds track routing/queuing time before requests are proxied to pods
	proxyQueueTimeWarningThreshold  = 4 * time.Second   // Warning threshold (unusually slow routing)
	proxyQueueTimeErrorThreshold    = 60 * time.Second  // Error threshold (severe routing delay)
	proxyQueueTimeCriticalThreshold = 180 * time.Second // Critical threshold (extreme routing delay)
)

// Feature gates for activator behavior
// These are loaded from the config-features ConfigMap at runtime
var (
	// maxTrackersPerRevision - Maximum number of pod trackers allowed per revision
	// Protects against memory exhaustion from excessive pod scaling
	// Each tracker uses ~80KB (breaker with 10,000 queue depth)
	// 5,000 trackers = ~400MB per revision, reasonable for production activators
	maxTrackersPerRevision = 5000
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
func setFeatureGatesForTesting(t interface {
	Helper()
	Cleanup(func())
}, qpAuthority, quarantine bool,
) {
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

// getFeatureGates safely reads the current feature gate values.
// Returns (enableQPAuthority, enableQuarantine).
func getFeatureGates() (bool, bool) {
	featureGateMutex.RLock()
	defer featureGateMutex.RUnlock()
	return enableQPAuthority, enableQuarantine
}

// Prometheus metrics for monitoring QP authoritative state system
var (
	revisionTrackerLimitExceeded = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "activator_revision_tracker_limit_exceeded_total",
			Help: "Total number of pod additions rejected due to exceeding maxTrackersPerRevision",
		},
		[]string{"namespace", "revision"},
	)
)

type breaker interface {
	Capacity() uint64
	Maybe(ctx context.Context, thunk func()) error
	UpdateConcurrency(uint64)
	Reserve(ctx context.Context) (func(), bool)
	Pending() int
	InFlight() uint64
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

func pickLBPolicy(loadBalancerPolicy *string, _ map[string]string, containerConcurrency uint64, logger *zap.SugaredLogger) (lbPolicy, string) {
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
	rt.mutatePodIncremental(podIP, eventType)
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
		revThrottler, err = newRevisionThrottler(
			revID,
			rev.Spec.LoadBalancingPolicy,
			rev.Spec.GetContainerConcurrency(), // Now returns uint64 directly
			pkgnet.ServicePortName(rev.GetProtocol()),
			queue.BreakerParams{QueueDepth: breakerQueueDepth, MaxConcurrency: revisionMaxConcurrency},
			t.logger,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create revision throttler: %w", err)
		}
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
		cc := rev.Spec.GetContainerConcurrency() // Now returns uint64 with validation
		newPolicy, name := pickLBPolicy(rev.Spec.LoadBalancingPolicy, nil, cc, t.logger)
		// Use atomic store for lock-free access in the hot request path
		rt.lbPolicy.Store(newPolicy)
		rt.containerConcurrency.Store(cc)
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
	if rt, ok := t.revisionThrottlers[revID]; ok {
		// Clean shutdown of the worker goroutine
		rt.Close()
		delete(t.revisionThrottlers, revID)
	}
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

// inferIndex returns the index of this activator slice.
// If inferIndex returns 0, it means that this activator will not receive
// any traffic just yet so, do not participate in slicing, this happens after
// startup, but before this activator is threaded into the endpoints
// (which is up to 10s after reporting healthy).
// For now we are just sorting the IP addresses of all activators
// and finding our index in that list.
func inferIndex(eps []string, ipAddress string) int {
	idx := sort.SearchStrings(eps, ipAddress)

	// Check if this activator is part of the endpoints slice?
	if idx == len(eps) || eps[idx] != ipAddress {
		return 0 // Return 0 as sentinel (not in endpoints)
	}
	return idx + 1 // Return 1-based index (1, 2, 3...)
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
// Routes cleanup through the work queue to avoid holding locks
func (t *Throttler) cleanupStaleTrackersOnce() {
	t.revisionThrottlersMutex.RLock()
	revisions := make([]*revisionThrottler, 0, len(t.revisionThrottlers))
	for _, rt := range t.revisionThrottlers {
		revisions = append(revisions, rt)
	}
	t.revisionThrottlersMutex.RUnlock()

	// Queue cleanup request for each revision through their work queues
	// This avoids holding locks and ensures cleanup is serialized with other state updates
	for _, rt := range revisions {
		// Non-blocking enqueue - if queue is full, skip this revision
		// (cleanup will happen on next periodic run)
		select {
		case rt.stateUpdateChan <- stateUpdateRequest{
			op:         opCleanupStalePods,
			enqueuedAt: time.Now().UnixNano(),
		}:
			// Successfully enqueued
			stateUpdateQueueDepth.WithLabelValues(rt.revID.Namespace, rt.revID.Name).Set(float64(len(rt.stateUpdateChan)))
		default:
			// Queue is full, skip this revision for now
			rt.logger.Warnw("Skipped stale tracker cleanup - queue full",
				"revision", rt.revID.String(),
				"queue-depth", len(rt.stateUpdateChan))
		}
	}
}
