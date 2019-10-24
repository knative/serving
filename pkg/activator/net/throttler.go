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
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/davecgh/go-spew/spew"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/resources"
)

type podIPTracker struct {
	dest     string
	requests int32
	weight   int
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

	// This will be non empty when we're able to use pod addressing.
	podIPTrackers []*podIPTracker

	// Effective trackers that are assigned to this Activator.
	// This is a subset of podIPTrackers.
	assignedTrackers []*podIPTracker

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

// pickP2C implements power of two choices algorithm for fast load balancing.
// See here: https://www.eecs.harvard.edu/~michaelm/postscripts/mythesis.pdf.
// The function picks a target to send request to, given list of `podIPTracker` objects, a
// callback to release the slot is returned as well.
func pickP2C(tgts []*podIPTracker) (string, func()) {
	var t1, t2 *podIPTracker
	switch len(tgts) {
	case 0:
		return "", nil
	case 1:
		t1, t2 = tgts[0], tgts[0]
	case 2:
		t1, t2 = tgts[0], tgts[1]
	default:
		i1 := rand.Intn(len(tgts))
		t1 = tgts[i1]
		i2 := rand.Intn(len(tgts))
		for i1 == i2 {
			i2 = rand.Intn(len(tgts))
		}
		t2 = tgts[i2]
	}
	// Note that this is not guaranteed to be precise, due to the case
	// that Load here and Add below are not atomic.
	if atomic.LoadInt32(&t1.requests) > atomic.LoadInt32(&t2.requests) {
		t1 = t2
	}

	// NB: we capture it as a variable, since `weight` might change
	// but we want it to be appropriately deducted later.
	w := int32(minOneOrValue(t1.weight))
	atomic.AddInt32(&t1.requests, w)
	return t1.dest, func() {
		atomic.AddInt32(&t1.requests, -w)
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

func noop() {}

// Returns a dest after incrementing its request count and a completion callback
// to be called after request completion. If no dest is found it returns "", nil.
func (rt *revisionThrottler) acquireDest() (string, func()) {
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	// This is intended to be called only after performing a read lock check on clusterIPDest
	if rt.clusterIPDest != "" {
		return rt.clusterIPDest, noop
	}
	return pickP2C(rt.assignedTrackers)
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

func (rt *revisionThrottler) resetTrackers() {
	for _, t := range rt.podIPTrackers {
		t.weight = 1
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
		if rt.clusterIPDest != "" {
			return 0
		}
		// Infifnite capacity, assign all.
		if rt.containerConcurrency == 0 {
			rt.assignedTrackers = rt.podIPTrackers
		} else {
			rt.resetTrackers()
			rt.assignedTrackers = assignSlice(rt.podIPTrackers, throttler.index(), ac)
		}
		rt.logger.Debugf("Trackers %d/%d  %s", throttler.index(), ac, spew.Sprint(rt.assignedTrackers))
		return len(rt.assignedTrackers)
	}()

	capacity := 0
	if numTrackers > 0 {
		// Capacity is computed based off of number of trackers,
		// when using pod direct routing. So capacity would be (#pods * concurrency) / #activators.
		// So, for example for #pods=7, #activators = 5, CC = 10, then each activator will get 14 concurrent
		// requests (70/5=14).
		// Now, each of the activators will get 3 assgined pod IP trackers, one with weight=1 and
		// two with weight of 5. This will ensure that the ones with weight 5 will get 5x fewer requests
		// than, so in this case exclusive pod will get 10 requests and non exclusive pods will get 2 requests
		// each (on average).
		capacity = rt.calculateCapacity(len(rt.podIPTrackers), ac, throttler.breakerParams.MaxConcurrency)
	} else {
		// Capacity is computed off of number of backends, when we are using clusterIP routing.
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

func (rt *revisionThrottler) updateThrottleState(
	throttler *Throttler, backendCount int,
	trackers []*podIPTracker, clusterIPDest string) {
	rt.logger.Infof("Updating Revision Throttler with: clusterIP = %q, trackers = %d, backends = %d activator pos %d/%d",
		clusterIPDest, len(trackers), backendCount, throttler.index(), throttler.activatorCount())

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

// pickIndices picks the indices for the slicing.
func pickIndices(numTrackers, selfIndex, numActivators int) (beginIndex, endIndex, tail int) {
	if numActivators > numTrackers {
		// 1. We have fewer pods than than activators. Assign the pod in round robin fashion.
		// NB: when we implement subsetting this will be less of a problem.
		// e.g. lt=3, #ac = 5; for selfIdx = 3 => 3 % 3 = 0, or for si = 5 => 5%3 = 2
		beginIndex = selfIndex % numTrackers
		endIndex = beginIndex + 1
		return
	}
	// 2. We have at least as many pods as activators. Assign equal slices
	// to the Activators. If the numbers don't divide evenly, assign the remnants
	// equally across the activators.
	sliceSize := numTrackers / numActivators
	tail = numTrackers % numActivators
	beginIndex = selfIndex * sliceSize
	endIndex = beginIndex + sliceSize
	return
}

// assignSlice picks a subset of the individual pods to send requests to
// for this Activator instance. This only matters in case of direct
// to pod IP routing, and is irrelevant, when ClusterIP is used.
func assignSlice(trackers []*podIPTracker, selfIndex, numActivators int) []*podIPTracker {
	// When we're unassigned, doesn't matter what we return.
	lt := len(trackers)
	if selfIndex == -1 || lt <= 1 {
		return trackers
	}
	// Sort, so we get more or less stable results.
	sort.Slice(trackers, func(i, j int) bool {
		return trackers[i].dest < trackers[j].dest
	})
	bi, ei, tail := pickIndices(lt, selfIndex, numActivators)

	// Those are the ones that belong exclusively to this activator.
	ret := append(trackers[:0:0], trackers[bi:ei]...)
	for i := len(trackers) - tail; i < len(trackers); i++ {
		t := trackers[i]
		// Those are going to be shared between
		// all the activators, so the weight is |numActivators|.
		t.weight = numActivators
		ret = append(ret, t)
	}
	return ret
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
		trackersMap := make(map[string]*podIPTracker, len(rt.podIPTrackers))
		for _, tracker := range rt.podIPTrackers {
			trackersMap[tracker.dest] = tracker
		}

		trackers := make([]*podIPTracker, 0, len(update.Dests))

		// Loop over dests, reuse existing tracker if we have one otherwise create
		// a new one.
		for newDest := range update.Dests {
			tracker, ok := trackersMap[newDest]
			if !ok {
				tracker = &podIPTracker{dest: newDest, weight: 1}
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

func (t *Throttler) handleUpdate(update revisionDestsUpdate) {
	if rt, err := t.getOrCreateRevisionThrottler(update.Rev); err != nil {
		if k8serrors.IsNotFound(err) {
			t.logger.Debugf("Revision %q is not found. Probably it was removed", update.Rev.String())
		} else {
			t.logger.Errorw(
				fmt.Sprintf("Failed to get revision throttler for revision %q", update.Rev.String()),
				zap.Error(err))
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
