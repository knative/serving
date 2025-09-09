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
	"math"
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
)

func newPodTracker(dest string, b breaker) *podTracker {
	tracker := &podTracker{
		id:        string(uuid.NewUUID()),
		createdAt: time.Now().UnixMicro(),
		dest:      dest,
		b:         b,
	}
	tracker.state.Store(uint32(podHealthy)) // Start in healthy state
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
func (p *podTracker) Reserve(ctx context.Context) (func(), bool) {
	// Increment ref count
	p.addRef()

	state := podState(p.state.Load())
	// Only healthy and pending pods can be reserved
	if state != podHealthy && state != podPending {
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
	lbPolicy             lbPolicy

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

func newRevisionThrottler(revID types.NamespacedName,
	containerConcurrency int, proto string,
	breakerParams queue.BreakerParams,
	logger *zap.SugaredLogger,
) *revisionThrottler {
	logger = logger.With(zap.String(logkey.Key, revID.String()))
	var (
		revBreaker breaker
		lbp        lbPolicy
	)

	// Default based on container concurrency
	if containerConcurrency == 0 {
		lbp = randomChoice2Policy
	} else {
		lbp = firstAvailableLBPolicy
	}

	if containerConcurrency == 0 {
		revBreaker = newInfiniteBreaker(logger)
	} else {
		revBreaker = queue.NewBreaker(breakerParams)
	}
	t := &revisionThrottler{
		revID:       revID,
		breaker:     revBreaker,
		lbPolicy:    lbp,
		logger:      logger,
		protocol:    proto,
		podTrackers: make(map[string]*podTracker),
	}
	// Handle negative or out-of-range values gracefully
	// Safe conversion: clamping to uint32 range
	var safeCC uint32
	if containerConcurrency <= 0 {
		safeCC = 0
	} else if containerConcurrency > math.MaxInt32 {
		// Clamp to a safe maximum value for container concurrency
		safeCC = math.MaxInt32
	} else {
		safeCC = uint32(containerConcurrency)
	}
	t.containerConcurrency.Store(safeCC)

	// Start with unknown
	t.activatorIndex.Store(-1)
	return t
}

func noop() {}

func (rt *revisionThrottler) acquireDest(ctx context.Context) (func(), *podTracker, bool) {
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	// Disabled clusterIP routing - always use pod routing
	// if rt.clusterIPTracker != nil {
	// 	return noop, rt.clusterIPTracker, true
	// }

	// Use assigned trackers directly
	if len(rt.assignedTrackers) == 0 {
		return noop, nil, false
	}
	callback, pt := rt.lbPolicy(ctx, rt.assignedTrackers)
	return callback, pt, false
}

func (rt *revisionThrottler) try(ctx context.Context, function func(dest string, isClusterIP bool) error) error {
	var ret error

	// Retrying infinitely as long as we receive no dest. Outer semaphore and inner
	// pod capacity are not changed atomically, hence they can race each other. We
	// "reenqueue" requests should that happen.
	reenqueue := true
	for reenqueue {
		reenqueue = false

		rt.mux.RLock()
		assignedTrackers := rt.assignedTrackers
		rt.mux.RUnlock()
		if len(assignedTrackers) == 0 {
			rt.logger.Debug("No Assigned trackers")
		}
		if err := rt.breaker.Maybe(ctx, func() {
			callback, tracker, isClusterIP := rt.acquireDest(ctx)
			if tracker == nil {
				// This can happen if individual requests raced each other or if pod
				// capacity was decreased after passing the outer semaphore.
				reenqueue = true
				rt.logger.Debug("Failed to acquire tracker")
				return
			}
			trackerID := tracker.id
			rt.logger.Infof("Acquired Pod Tracker %s - %s (createdAt %d)", trackerID, tracker.dest, tracker.createdAt)
			rt.logger.Debugf("Tracker %s Breaker State: capacity: %d, inflight: %d, pending: %d", trackerID, tracker.Capacity(), tracker.InFlight(), tracker.Pending())
			defer func() {
				callback()
				rt.logger.Debugf("%s breaker release semaphore", trackerID)
			}()
			// We already reserved a guaranteed spot. So just execute the passed functor.

			ret = function(tracker.dest, isClusterIP)
		}); err != nil {
			return err
		}
		rt.logger.Debug("Reenqueue request")
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
	rt.logger.Infof("Set capacity to %d (backends: %d, index: %d/%d)",
		capacity, backendCount, ai, ac)

	// Handle negative or out-of-range values gracefully
	// Safe conversion: clamping to uint32 range
	var safeBackendCount uint32
	if backendCount <= 0 {
		safeBackendCount = 0
	} else if backendCount > math.MaxInt32 {
		// Clamp to a safe maximum value
		safeBackendCount = math.MaxInt32
	} else {
		safeBackendCount = uint32(backendCount)
	}
	rt.backendCount.Store(safeBackendCount)
	rt.breaker.UpdateConcurrency(capacity)
}

func (rt *revisionThrottler) updateThrottlerState(backendCount int, newTrackers []*podTracker, healthyDests []string, drainingDests []string, clusterIPDest *podTracker) {
	defer func() {
		if r := recover(); r != nil {
			rt.logger.Errorf("Panic in revisionThrottler.updateThrottlerState: %v", r)
			panic(r)
		}
	}()

	rt.logger.Infof("Updating Throttler %s: trackers = %d, backends = %d",
		rt.revID, len(newTrackers), backendCount)
	rt.logger.Infof("Throttler %s DrainingDests: %s", rt.revID, drainingDests)
	rt.logger.Infof("Throttler %s healthyDests: %s", rt.revID, healthyDests)

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
				// Only transition to healthy if not already healthy
				if currentState != podHealthy {
					tracker.state.Store(uint32(podHealthy))
					tracker.drainingStartTime.Store(0)
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
			case podHealthy:
				if tracker.tryDrain() {
					rt.logger.Infof("Pod %s transitioning to draining state, refCount=%d", d, tracker.getRefCount())
					if tracker.getRefCount() == 0 {
						tracker.state.Store(uint32(podRemoved))
						delete(rt.podTrackers, d)
						rt.logger.Infof("Pod %s removed immediately (no active requests)", d)
					}
				}
			case podDraining:
				refCount := tracker.getRefCount()
				if refCount == 0 {
					tracker.state.Store(uint32(podRemoved))
					delete(rt.podTrackers, d)
					rt.logger.Infof("Pod %s removed after draining (no active requests)", d)
				} else {
					drainingStart := tracker.drainingStartTime.Load()
					if drainingStart > 0 && now-drainingStart > int64(maxDrainingDuration.Seconds()) {
						rt.logger.Warnf("Force removing pod %s stuck in draining state for %d seconds, refCount=%d", d, now-drainingStart, refCount)
						tracker.state.Store(uint32(podRemoved))
						delete(rt.podTrackers, d)
					}
				}
			case podPending:
				// Pod being removed while still pending health check
				tracker.state.Store(uint32(podRemoved))
				delete(rt.podTrackers, d)
				rt.logger.Infof("Pod %s removed while pending health check", d)
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
			} else if tracker.state.Load() != uint32(podHealthy) {
				// Pod was previously in a non-healthy state, set to pending for re-validation
				tracker.state.Store(uint32(podPending))
				tracker.drainingStartTime.Store(0)
				rt.logger.Infow("Re-adding previously unhealthy pod as pending",
					"dest", newDest,
					"previousState", podState(tracker.state.Load()))
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
func (t *Throttler) Try(ctx context.Context, revID types.NamespacedName, function func(string, bool) error) error {
	rt, err := t.getOrCreateRevisionThrottler(revID)
	if err != nil {
		return err
	}
	return rt.try(ctx, function)
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

	if _, err := t.getOrCreateRevisionThrottler(revID); err != nil {
		t.logger.Errorw("Failed to get revision throttler for revision",
			zap.Error(err), zap.String(logkey.Key, revID.String()))
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
	// newNA comes from len() so it's always >= 0, but we need to validate for gosec
	// Safe conversion: newNA is from len(epsL) which is always non-negative
	var safeNA uint32
	if newNA < 0 {
		// This should never happen since newNA comes from len()
		rt.logger.Errorf("Unexpected negative value for newNA: %d", newNA)
		return
	}
	safeNA = uint32(newNA)

	if na == safeNA && ai == newAI {
		// The state didn't change, do nothing
		return
	}

	rt.numActivators.Store(safeNA)
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
