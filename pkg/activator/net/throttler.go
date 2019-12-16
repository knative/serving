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
	"errors"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/resources"
)

type podTracker struct {
	dest string
	b    breaker
}

func (p *podTracker) Capacity() int {
	if p.b == nil {
		return 1
	}
	return p.b.Capacity()
}

func (p *podTracker) UpdateConcurrency(c int) error {
	if p.b == nil {
		return nil
	}
	return p.b.UpdateConcurrency(c)
}

func (p *podTracker) Reserve(ctx context.Context) (func(), bool) {
	if p.b == nil {
		return noop, true
	}
	return p.b.Reserve(ctx)
}

type breaker interface {
	Capacity() int
	Maybe(ctx context.Context, thunk func()) error
	UpdateConcurrency(int) error
	Reserve(ctx context.Context) (func(), bool)
}

var ErrActivatorOverload = errors.New("activator overload")

type revisionThrottler struct {
	revID                types.NamespacedName
	containerConcurrency int

	// Holds the current number of backends. This is used for when we get an activatorCount update and
	// therefore need to recalculate capacity
	backendCount int

	// This is a breaker for the revision as a whole. try calls first pass through
	// this breaker and are either called with clusterIPDest or go through selecting
	// a podIPTracker and are then called.
	breaker breaker

	// This will be non empty when we're able to use pod addressing.
	podTrackers []*podTracker

	// Effective trackers that are assigned to this Activator.
	// This is a subset of podIPTrackers.
	assignedTrackers []*podTracker

	// If we dont have a healthy clusterIPTracker this is set to nil, otherwise
	// it is the l4dest for this revision's private clusterIP.
	clusterIPTracker *podTracker

	// mux guards "throttle state" which is the state we use during the request path. This
	// is trackers, clusterIPDest.
	mux sync.RWMutex

	// used to atomically calculate and set capacity
	capacityMux sync.Mutex

	logger *zap.SugaredLogger
}

func newRevisionThrottler(revID types.NamespacedName,
	containerConcurrency int,
	breakerParams queue.BreakerParams,
	logger *zap.SugaredLogger) *revisionThrottler {
	logger = logger.With(zap.String(logkey.Key, revID.String()))
	var revBreaker breaker
	if containerConcurrency == 0 {
		revBreaker = newInfiniteBreaker(logger)
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

func noop() {}

// pickPod picks the first tracker that has open capacity if container concurrency
// if limited, random pod otherwise.
func pickPod(ctx context.Context, tgs []*podTracker, cc int) (func(), *podTracker) {
	// Infinite capacity, pick random. We have to do this
	// otherwise _all_ the requests will go to the first pod
	// since it has unlimited capacity.
	if cc == 0 {
		return noop, tgs[rand.Intn(len(tgs))]
	}
	for _, t := range tgs {
		if cb, ok := t.Reserve(ctx); ok {
			return cb, t
		}
	}
	// NB: as currently written this can never happen.
	return noop, nil
}

// Returns a dest that at the moment of choosing had an open slot
// for request.
func (rt *revisionThrottler) acquireDest(ctx context.Context) (func(), *podTracker) {
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	if rt.clusterIPTracker != nil {
		return noop, rt.clusterIPTracker
	}
	return pickPod(ctx, rt.assignedTrackers, rt.containerConcurrency)
}

func (rt *revisionThrottler) try(ctx context.Context, function func(string) error) error {
	var ret error

	if err := rt.breaker.Maybe(ctx, func() {
		cb, tracker := rt.acquireDest(ctx)
		if tracker == nil {
			ret = errors.New("made it through breaker but we have no clusterIP or podIPs. This should" +
				" never happen" + rt.revID.String())
			return
		}
		defer cb()
		// We already reserved a guaranteed spot. So just execute the passed functor.
		ret = function(tracker.dest)
	}); err != nil {
		return err
	}
	return ret
}

func (rt *revisionThrottler) calculateCapacity(size, activatorCount, maxConcurrency int) int {
	targetCapacity := rt.containerConcurrency * size

	if size > 0 && (rt.containerConcurrency == 0 || targetCapacity > maxConcurrency) {
		// If cc==0, we need to pick a number, but it does not matter, since
		// infinite breaker will dole out as many tokens as it can.
		targetCapacity = maxConcurrency
	} else if targetCapacity > 0 {
		targetCapacity = minOneOrValue(targetCapacity / minOneOrValue(activatorCount))
	}

	return targetCapacity
}

// This makes sure we reset the capacity to the CC, since the pod
// might be reassiged to be exclusively used.
func (rt *revisionThrottler) resetTrackers() {
	if rt.containerConcurrency <= 0 {
		return
	}
	for _, t := range rt.podTrackers {
		// Reset to default.
		t.UpdateConcurrency(rt.containerConcurrency)
	}
}

func (rt *revisionThrottler) updateCapacity(throttler *Throttler, backendCount int) {
	ac := throttler.activatorCount()

	// We have to make assignments on each updateCapacity, since if number
	// of activators changes, then we need to rebalance the assignedTrackers.
	numTrackers := func() int {
		rt.mux.Lock()
		defer rt.mux.Unlock()
		// We're using cluster IP.
		if rt.clusterIPTracker != nil {
			return 0
		}
		// Infifnite capacity, assign all.
		if rt.containerConcurrency == 0 {
			rt.assignedTrackers = rt.podTrackers
		} else {
			rt.resetTrackers()
			rt.assignedTrackers = assignSlice(rt.podTrackers, throttler.index(), ac, rt.containerConcurrency)
		}
		rt.logger.Debugf("Trackers %d/%d  %v", throttler.index(), ac, rt.assignedTrackers)
		return len(rt.assignedTrackers)
	}()

	capacity := 0
	if numTrackers > 0 {
		// Capacity is computed based off of number of trackers,
		// when using pod direct routing.
		capacity = rt.calculateCapacity(len(rt.podTrackers), ac, throttler.breakerParams.MaxConcurrency)
	} else {
		// Capacity is computed off of number of ready backends,
		// when we are using clusterIP routing.
		capacity = rt.calculateCapacity(backendCount, ac, throttler.breakerParams.MaxConcurrency)
	}
	rt.logger.Infof("Set capacity to %d (backends: %d, index: %d/%d)",
		capacity, backendCount, throttler.index(), ac)

	// TODO(vagababov): analyze to see if we need this mutex at all?
	rt.capacityMux.Lock()
	defer rt.capacityMux.Unlock()

	rt.backendCount = backendCount
	rt.breaker.UpdateConcurrency(capacity)
}

func (rt *revisionThrottler) updateThrottlerState(
	throttler *Throttler, backendCount int,
	trackers []*podTracker, clusterIPDest *podTracker) {
	rt.logger.Infof("Updating Revision Throttler with: clusterIP = %v, trackers = %d, backends = %d activator pos %d/%d",
		clusterIPDest, len(trackers), backendCount, throttler.index(), throttler.activatorCount())

	// Update trackers / clusterIP before capacity. Otherwise we can race updating our breaker when
	// we increase capacity, causing a request to fall through before a tracker is added, causing an
	// incorrect LB decision.
	if func() bool {
		rt.mux.Lock()
		defer rt.mux.Unlock()
		rt.podTrackers = trackers
		rt.clusterIPTracker = clusterIPDest
		return clusterIPDest != nil || len(trackers) > 0
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

// pickIndices picks the indices for the slicing.
func pickIndices(numTrackers, selfIndex, numActivators int) (beginIndex, endIndex, remnants int) {
	if numActivators > numTrackers {
		// 1. We have fewer pods than than activators. Assign the pod in round robin fashion.
		// NB: when we implement subsetting this will be less of a problem.
		// e.g. lt=3, #ac = 5; for selfIdx = 3 => 3 % 3 = 0, or for si = 5 => 5%3 = 2
		beginIndex = selfIndex % numTrackers
		endIndex = beginIndex + 1
		return
	}

	// 2. distribute equally and share the remnants
	// among all the activatos, but with reduced capacity, if finite.
	sliceSize := numTrackers / numActivators
	remnants = numTrackers % numActivators
	beginIndex = selfIndex * sliceSize
	endIndex = beginIndex + sliceSize
	return
}

// assignSlice picks a subset of the individual pods to send requests to
// for this Activator instance. This only matters in case of direct
// to pod IP routing, and is irrelevant, when ClusterIP is used.
func assignSlice(trackers []*podTracker, selfIndex, numActivators, cc int) []*podTracker {
	// When we're unassigned, doesn't matter what we return.
	lt := len(trackers)
	if selfIndex == -1 || lt <= 1 {
		return trackers
	}
	// Sort, so we get more or less stable results.
	sort.Slice(trackers, func(i, j int) bool {
		return trackers[i].dest < trackers[j].dest
	})
	bi, ei, remnants := pickIndices(lt, selfIndex, numActivators)
	x := append(trackers[:0:0], trackers[bi:ei]...)
	if remnants > 0 {
		tail := trackers[len(trackers)-remnants:]
		// We shuffle the tail, to ensure that pods in the tail get better
		// load distribution, since we sort the pods above, this puts more requests
		// on the very first tail pod, than on the others.
		rand.Shuffle(remnants, func(i, j int) {
			tail[i], tail[j] = tail[j], tail[i]
		})
		// We need minOneOrValue in order for cc==0 to work.
		dcc := minOneOrValue(int(math.Ceil(float64(cc) / float64(numActivators))))
		// This is basically: x = append(x, trackers[len(trackers)-remnants:]...)
		// But we need to update the capacity.
		for _, t := range tail {
			t.UpdateConcurrency(dcc)
			x = append(x, t)
		}
	}
	return x
}

// This function will never be called in parallel but try can be called in parallel to this so we need
// to lock on updating concurrency / trackers
func (rt *revisionThrottler) handleUpdate(throttler *Throttler, update revisionDestsUpdate) {
	rt.logger.Debugf("Handling update w/ ClusterIP=%q, %d ready and dests: %v",
		update.ClusterIPDest, len(update.Dests), update.Dests)

	// ClusterIP is not yet ready, so we want to send requests directly to the pods.
	// NB: this will not be called in parallel, thus we can build a new podIPTrackers
	// array before taking out a lock.
	if update.ClusterIPDest == "" {
		// Create a map for fast lookup of existing trackers.
		trackersMap := make(map[string]*podTracker, len(rt.podTrackers))
		for _, tracker := range rt.podTrackers {
			trackersMap[tracker.dest] = tracker
		}

		trackers := make([]*podTracker, 0, len(update.Dests))

		// Loop over dests, reuse existing tracker if we have one, otherwise create
		// a new one.
		for newDest := range update.Dests {
			tracker, ok := trackersMap[newDest]
			if !ok {
				if rt.containerConcurrency == 0 {
					tracker = &podTracker{dest: newDest}
				} else {
					tracker = &podTracker{
						dest: newDest,
						b: queue.NewBreaker(queue.BreakerParams{
							QueueDepth:      throttler.breakerParams.QueueDepth,
							MaxConcurrency:  rt.containerConcurrency,
							InitialCapacity: rt.containerConcurrency, // Presume full unused capacity.
						}),
					}
				}
			}
			trackers = append(trackers, tracker)
		}

		rt.updateThrottlerState(throttler, len(update.Dests), trackers, nil /*clusterIP*/)
		return
	}

	rt.updateThrottlerState(throttler, len(update.Dests), nil /*trackers*/, &podTracker{
		dest: update.ClusterIPDest,
	})
}

// Throttler load balances requests to revisions based on capacity. When `Run` is called it listens for
// updates to revision backends and decides when and when and where to forward a request.
type Throttler struct {
	revisionThrottlers      map[types.NamespacedName]*revisionThrottler
	revisionThrottlersMutex sync.RWMutex
	breakerParams           queue.BreakerParams
	revisionLister          servinglisters.RevisionLister
	numActivators           int32  // Total number of activators.
	activatorIndex          int32  // The assigned index of this activator, -1 is Activator is not expected to receive traffic.
	ipAddress               string // The IP address of this activator.
	logger                  *zap.SugaredLogger
}

// NewThrottler creates a new Throttler
func NewThrottler(ctx context.Context,
	breakerParams queue.BreakerParams,
	ipAddr string) *Throttler {
	revisionInformer := revisioninformer.Get(ctx)
	t := &Throttler{
		revisionThrottlers: make(map[types.NamespacedName]*revisionThrottler),
		breakerParams:      breakerParams,
		revisionLister:     revisionInformer.Lister(),
		ipAddress:          ipAddr,
		activatorIndex:     -1, // Unset yet.
		logger:             logging.FromContext(ctx),
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

// Run starts the throttler and blocks until the context is done.
func (t *Throttler) Run(ctx context.Context) {
	rbm := newRevisionBackendsManager(ctx, network.AutoTransport)
	// Update channel is closed when ctx is done.
	t.run(rbm.updates())
}

func (t *Throttler) run(updateCh <-chan revisionDestsUpdate) {
	for update := range updateCh {
		t.handleUpdate(update)
	}
	t.logger.Info("The Throttler has stopped.")
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
		revThrottler = newRevisionThrottler(revID, int(rev.Spec.GetContainerConcurrency()), t.breakerParams, t.logger)
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

func (t *Throttler) handleUpdate(update revisionDestsUpdate) {
	if rt, err := t.getOrCreateRevisionThrottler(update.Rev); err != nil {
		if k8serrors.IsNotFound(err) {
			t.logger.Debugf("Revision %q is not found. Probably it was removed", update.Rev.String())
		} else {
			t.logger.With(zap.Error(err)).Errorf(
				"Failed to get revision throttler for revision %q", update.Rev)
		}
	} else {
		rt.handleUpdate(t, update)
	}
}

// inferIndex returns the index of this activator slice.
// If inferIndex returns -1, it means that this activator will not recive
// any traffic just yet so, do not participate in slicing, this happens after
// startup, but before this activator is threaded into the endpoints
// (which is up to 10s after reporting healthy).
// For now we are just sorting the IP addresses of all activators
// and finding our index in that list.
func inferIndex(eps []string, ipAddress string) int {
	// `eps` will contain port, so binary search of the insertion point would be fine.
	idx := sort.SearchStrings(eps, ipAddress)

	// Check if this activator is part of the endpoints slice?
	if idx == len(eps) || eps[idx] != ipAddress {
		idx = -1
	}
	return idx
}

func (t *Throttler) updateAllThrottlerCapacity() {
	t.revisionThrottlersMutex.RLock()
	defer t.revisionThrottlersMutex.RUnlock()

	for _, rt := range t.revisionThrottlers {
		rt.updateCapacity(t, rt.backendCount)
	}
}

func (t *Throttler) activatorEndpointsUpdated(newObj interface{}) {
	endpoints := newObj.(*corev1.Endpoints)

	// We want to pass sorted list, so that we get _some_ stability in the results.
	eps := endpointsToDests(endpoints, networking.ServicePortNameHTTP1).List()
	t.logger.Debugf("All Activator IPS: %v, my IP: %s", eps, t.ipAddress)
	idx := inferIndex(eps, t.ipAddress)
	activatorCount := resources.ReadyAddressCount(endpoints)
	t.logger.Infof("Got %d ready activator endpoints, our position is: %d", activatorCount, idx)
	atomic.StoreInt32(&t.numActivators, int32(activatorCount))
	atomic.StoreInt32(&t.activatorIndex, int32(idx))
	t.updateAllThrottlerCapacity()
}

func (t *Throttler) index() int {
	return int(atomic.LoadInt32(&t.activatorIndex))
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
	// `concurrency` should only be manipulated by `sync/atomic` methods.
	concurrency int32

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
func (ib *infiniteBreaker) Capacity() int {
	return int(atomic.LoadInt32(&ib.concurrency))
}

func zeroOrOne(x int) int32 {
	if x == 0 {
		return 0
	}
	return 1
}

// UpdateConcurrency sets the concurrency of the breaker
func (ib *infiniteBreaker) UpdateConcurrency(cc int) error {
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
		ib.logger.Infof("Context is closed: %v", ctx.Err())
		return ctx.Err()
	}
}

func (ib *infiniteBreaker) Reserve(context.Context) (func(), bool) { return noop, true }
