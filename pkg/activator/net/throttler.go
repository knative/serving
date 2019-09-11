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

package activator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	servinginformers "knative.dev/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/resources"
)

type podIPTracker struct {
	dest     string
	requests int32
}

type breaker interface {
	Capacity() int
	Maybe(ctx context.Context, thunk func()) error
	UpdateConcurrency(int) error
}

var ErrActivatorOverload = errors.New("activator overload")

type revisionThrottler struct {
	revID                types.NamespacedName
	containerConcurrency int64

	// Holds the current number of backends. This is used for when we get an activatorCount update and
	// therefore need to recalculate capacity
	backendCount int

	// This is a breaker for the revision as a whole. try calls first pass through
	// this breaker and are either called with clusterIPDest or go through selecting
	// a podIPTracker and are then called.
	breaker breaker

	// If we have a healthy clusterIPDest this is set to nil.
	podIPTrackers []*podIPTracker

	// If we dont have a healthy clusterIPDest this is set to the default (""), otherwise
	// it is the l4dest for this revision's private clusterIP
	clusterIPDest string

	// mux guards "throttle state" which is the state we use during the request path. This
	// is trackers, clusterIPDest
	mux sync.RWMutex

	// used to atomically calculate and set capacity
	capacityMux sync.Mutex

	logger *zap.SugaredLogger
}

func newRevisionThrottler(revID types.NamespacedName,
	containerConcurrency int64,
	breakerParams queue.BreakerParams,
	logger *zap.SugaredLogger) *revisionThrottler {
	logger = logger.With(zap.String(logkey.Key, revID.String()))
	var revBreaker breaker
	if containerConcurrency == 0 {
		revBreaker = NewInfiniteBreaker(logger)
	} else {
		revBreaker = queue.NewBreaker(breakerParams)
	}
	return &revisionThrottler{
		revID:                revID,
		containerConcurrency: containerConcurrency,
		breaker:              revBreaker,
		logger:               logger,
	}
}

// Returns clusterIP if it is the current valid dest. If neither clusterIP or podIPs are valid
// dests returns error.
func (rt *revisionThrottler) checkClusterIPDest() (string, error) {
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	if rt.clusterIPDest == "" && len(rt.podIPTrackers) == 0 {
		return "", errors.New("made it through breaker but we have no clusterIP or podIPs. This should" +
			" never happen" + rt.revID.String())
	}

	return rt.clusterIPDest, nil
}

// Returns a dest after incrementing its request count and a completion callback
// to be called after request completion. If no dest is found it returns "", nil.
func (rt *revisionThrottler) acquireDest() (string, func()) {
	var leastConn *podIPTracker
	clusterIPDest := func() string {
		rt.mux.Lock()
		defer rt.mux.Unlock()

		// This is intended to be called only after performing a read lock check on clusterIPDest
		if rt.clusterIPDest != "" {
			return rt.clusterIPDest
		}

		// Find the dest with fewest active connections.
		for _, tracker := range rt.podIPTrackers {
			if leastConn == nil || atomic.LoadInt32(&leastConn.requests) > tracker.requests {
				leastConn = tracker
			}
		}

		return ""
	}()
	if clusterIPDest != "" {
		return clusterIPDest, func() {}
	}

	if leastConn != nil {
		atomic.AddInt32(&leastConn.requests, 1)
		return leastConn.dest, func() {
			atomic.AddInt32(&leastConn.requests, -1)
		}
	}
	return "", nil
}

func (rt *revisionThrottler) try(ctx context.Context, function func(string) error) error {
	var ret error

	if err := rt.breaker.Maybe(ctx, func() {
		// See if we can get by with only a readlock
		dest, err := rt.checkClusterIPDest()
		if err != nil {
			ret = err
			return
		}

		if dest != "" {
			ret = function(dest)
			return
		}

		// Try again with a write lock falling back to a podIP dest
		dest, completionCb := rt.acquireDest()
		if dest == "" {
			ret = errors.New("no podIP destination found, this should never happen")
			return
		}

		defer completionCb()
		ret = function(dest)
	}); err != nil {
		return err
	}
	return ret
}

func (rt *revisionThrottler) calculateCapacity(size, activatorCount, maxConcurrency int) int {
	targetCapacity := int(rt.containerConcurrency) * size

	if size > 0 && (rt.containerConcurrency == 0 || targetCapacity > maxConcurrency) {
		// If cc==0, we need to pick a number, but it does not matter, since
		// infinite breaker will dole out as many tokens as it can.
		targetCapacity = maxConcurrency
	} else if targetCapacity > 0 {
		targetCapacity = minOneOrValue(targetCapacity / minOneOrValue(activatorCount))
	}

	return targetCapacity
}

func (rt *revisionThrottler) updateCapacity(throttler *Throttler, backendCount int) {
	rt.capacityMux.Lock()
	defer rt.capacityMux.Unlock()

	ac := throttler.activatorCount()
	capacity := rt.calculateCapacity(backendCount, ac, throttler.breakerParams.MaxConcurrency)
	rt.backendCount = backendCount
	rt.breaker.UpdateConcurrency(capacity)
	rt.logger.Debugf("Set capacity to %d (backends: %d, activators: %d)", capacity, backendCount, ac)
}

func (rt *revisionThrottler) updateThrottleState(throttler *Throttler, backendCount int, trackers []*podIPTracker, clusterIPDest string) {
	rt.logger.Infof("Updating Revision Throttler with: clusterIP = %s, trackers = %d, backends = %d",
		clusterIPDest, len(trackers), backendCount)

	// Update trackers / clusterIP before capacity. Otherwise we can race updating our breaker when
	// we increase capacity, causing a request to fall through before a tracker is added, causing an
	// incorrect LB decision.
	if func() bool {
		rt.mux.Lock()
		defer rt.mux.Unlock()
		rt.podIPTrackers = trackers
		rt.clusterIPDest = clusterIPDest
		return clusterIPDest != "" || len(trackers) > 0
	}() {
		// If we have an address to target, then pass through an accurate
		// accounting of the number of backends.
		rt.updateCapacity(throttler, backendCount)
	} else {
		// If we do not have an address to target, then we should treat it
		// as though we have zero backends.
		rt.updateCapacity(throttler, 0)
	}
}

// This function will never be called in parallel but try can be called in parallel to this so we need
// to lock on updating concurrency / trackers
func (rt *revisionThrottler) handleUpdate(throttler *Throttler, update *RevisionDestsUpdate) {
	rt.logger.Debugf("Handling update w/ %d ready and dests: %v", len(update.Dests), update.Dests)

	// ClusterIP is not yet ready, so we want to send requests directly to the pods.
	// NB: this will not be called in parallel, thus we can build a new podIPTrackers
	// array before taking out a lock.
	if update.ClusterIPDest == "" {
		// Create a map for fast lookup of existing trackers
		trackersMap := make(map[string]*podIPTracker, len(rt.podIPTrackers))
		for _, tracker := range rt.podIPTrackers {
			trackersMap[tracker.dest] = tracker
		}

		trackers := make([]*podIPTracker, 0, len(update.Dests))

		// Loop over dests, reuse existing tracker if we have one otherwise create new
		for _, newDest := range update.Dests {
			tracker, ok := trackersMap[newDest]
			if !ok {
				tracker = &podIPTracker{dest: newDest}
			}
			trackers = append(trackers, tracker)
		}

		rt.updateThrottleState(throttler, len(update.Dests), trackers, "" /*clusterIP*/)
		return
	}

	rt.updateThrottleState(throttler, len(update.Dests), nil /*trackers*/, update.ClusterIPDest)
}

// Throttler load balances requests to revisions based on capacity. When `Run` is called it listens for
// updates to revision backends and decides when and when and where to forward a request.
type Throttler struct {
	revisionThrottlers      map[types.NamespacedName]*revisionThrottler
	revisionThrottlersMutex sync.RWMutex
	breakerParams           queue.BreakerParams
	revisionLister          servinglisters.RevisionLister
	numActivators           int32
	logger                  *zap.SugaredLogger
}

// NewThrottler creates a new Throttler
func NewThrottler(breakerParams queue.BreakerParams,
	revisionInformer servinginformers.RevisionInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	logger *zap.SugaredLogger) *Throttler {
	t := &Throttler{
		revisionThrottlers: make(map[types.NamespacedName]*revisionThrottler),
		breakerParams:      breakerParams,
		revisionLister:     revisionInformer.Lister(),
		logger:             logger,
	}

	// Watch revisions to create throttler with backlog immediately and delete
	// throttlers on revision delete
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.revisionUpdated,
		UpdateFunc: controller.PassNew(t.revisionUpdated),
		DeleteFunc: t.revisionDeleted,
	})

	// Watch activator endpoint to maintain activator count
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.NameFilterFunc(networking.ActivatorServiceName),
			reconciler.NamespaceFilterFunc(system.Namespace()),
		),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    t.activatorEndpointsUpdated,
			UpdateFunc: controller.PassNew(t.activatorEndpointsUpdated),
		},
	})

	return t
}

// Run starts the throttler and blocks until updateCh is closed
func (t *Throttler) Run(updateCh <-chan *RevisionDestsUpdate) {
	for update := range updateCh {
		t.handleUpdate(update)
	}
}

// Try waits for capacity and then executes function, passing in a l4 dest to send a request
func (t *Throttler) Try(ctx context.Context, revID types.NamespacedName, function func(string) error) error {
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
		revThrottler = newRevisionThrottler(revID, rev.Spec.GetContainerConcurrency(), t.breakerParams, t.logger)
		t.revisionThrottlers[revID] = revThrottler
	}
	return revThrottler, nil
}

// revisionUpdated is used to ensure we have a backlog set up for a revision as soon as it is created
// rather than erroring with revision not found until a networking probe succeeds
func (t *Throttler) revisionUpdated(obj interface{}) {
	rev := obj.(*v1alpha1.Revision)
	revID := types.NamespacedName{rev.Namespace, rev.Name}
	t.logger.Debugf("Revision update %q", revID.String())

	if _, err := t.getOrCreateRevisionThrottler(revID); err != nil {
		t.logger.Errorw("Failed to get revision throttler for revision "+revID.String(), zap.Error(err))
	}
}

// revisionDeleted is to clean up revision throttlers after a revision is deleted to prevent unbounded
// memory growth
func (t *Throttler) revisionDeleted(obj interface{}) {
	rev := obj.(*v1alpha1.Revision)
	revID := types.NamespacedName{rev.Namespace, rev.Name}
	t.logger.Debugf("Revision delete %q", revID.String())

	t.revisionThrottlersMutex.Lock()
	defer t.revisionThrottlersMutex.Unlock()
	delete(t.revisionThrottlers, revID)
}

func (t *Throttler) handleUpdate(update *RevisionDestsUpdate) {
	if rt, err := t.getOrCreateRevisionThrottler(update.Rev); err != nil {
		t.logger.Errorw(fmt.Sprintf("Failed to get revision throttler for revision %q", update.Rev.String()),
			zap.Error(err))
	} else {
		rt.handleUpdate(t, update)
	}
}

func (t *Throttler) updateAllThrottlerCapacity() {
	t.logger.Debugf("Updating activator count to %d.", t.activatorCount())
	t.revisionThrottlersMutex.RLock()
	defer t.revisionThrottlersMutex.RUnlock()

	for _, rt := range t.revisionThrottlers {
		t.logger.Debugf("Updating rt %v with backend count %d", rt, rt.backendCount)
		rt.updateCapacity(t, rt.backendCount)
	}
}

func (t *Throttler) activatorEndpointsUpdated(newObj interface{}) {
	endpoints := newObj.(*corev1.Endpoints)

	activatorCount := resources.ReadyAddressCount(endpoints)
	t.logger.Debugf("Got %d ready activator endpoints.", activatorCount)
	atomic.StoreInt32(&t.numActivators, int32(activatorCount))
	t.updateAllThrottlerCapacity()
}

func (t *Throttler) activatorCount() int {
	return int(atomic.LoadInt32(&t.numActivators))
}

// minOneOrValue function returns num if its greater than 1
// else the function returns 1
func minOneOrValue(num int) int {
	if num > 1 {
		return num
	}
	return 1
}

// InfiniteBreaker is basically a short circuit.
// InfiniteBreaker provides us capability to send unlimited number
// of requests to the downstream system.
// This is to be used only when the container concurrency is unset
// (i.e. infinity).
// The InfiniteBreaker will, though, block the requests when
// downstream capacity is 0.
// TODO(greghaynes) When the old throttler is removed this struct can be private.
type InfiniteBreaker struct {
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
	// `concurrency` should only be manipulated by `sync/atomic` methods.
	concurrency int32

	logger *zap.SugaredLogger
}

// NewInfiniteBreaker creates an InfiniteBreaker
func NewInfiniteBreaker(logger *zap.SugaredLogger) *InfiniteBreaker {
	return &InfiniteBreaker{
		broadcast: make(chan struct{}),
		logger:    logger,
	}
}

// Capacity returns the current capacity of the breaker
func (ib *InfiniteBreaker) Capacity() int {
	return int(atomic.LoadInt32(&ib.concurrency))
}

func zeroOrOne(x int) int32 {
	if x == 0 {
		return 0
	}
	return 1
}

// UpdateConcurrency sets the concurrency of the breaker
func (ib *InfiniteBreaker) UpdateConcurrency(cc int) error {
	rcc := zeroOrOne(cc)
	// We lock here to make sure two scale up events don't
	// stomp on each other's feet.
	ib.mu.Lock()
	defer ib.mu.Unlock()
	old := atomic.SwapInt32(&ib.concurrency, rcc)

	// Scale up/down event.
	if old != rcc {
		if rcc == 0 {
			// Scaled to 0.
			ib.broadcast = make(chan struct{})
		} else {
			close(ib.broadcast)
		}
	}
	return nil
}

// Maybe executes thunk when capacity is available
func (ib *InfiniteBreaker) Maybe(ctx context.Context, thunk func()) error {
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
		ib.logger.Infof("Context is closed: %v", ctx.Err())
		return ctx.Err()
	}
}
