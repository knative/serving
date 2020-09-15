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
	"math/rand"
	"sort"
	"sync"

	"go.uber.org/atomic"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"knative.dev/networking/pkg/apis/networking"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"
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
)

type podTracker struct {
	dest string
	b    breaker
	// weight is used for LB policy implementations.
	weight atomic.Int32
}

func (p *podTracker) addWeight(w int32) {
	p.weight.Add(w)
}

func (p *podTracker) getWeight() int32 {
	return p.weight.Load()
}

func (p *podTracker) String() string {
	return p.dest
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

// revisionThrottler is used to throttle requests across the entire revision.
// We use a breaker across the entire revision as well as individual
// podTrackers because we need to queue requests in case no individual
// podTracker has available slots (when CC!=0).
type revisionThrottler struct {
	revID                types.NamespacedName
	containerConcurrency int
	lbPolicy             lbPolicy

	// These are used in slicing to infer which pods to assign
	// to this activator.
	numActivators atomic.Int32
	// If -1, it is presumed that this activator should not receive requests
	// for the revision. But due to the system being distributed it might take
	// time for everything to propagate. Thus when this is -1 we assign all the
	// pod trackers.
	activatorIndex atomic.Int32
	protocol       string

	// Holds the current number of backends. This is used for when we get an activatorCount update and
	// therefore need to recalculate capacity
	backendCount int

	// This is a breaker for the revision as a whole.
	breaker breaker

	// This will be non-empty when we're able to use pod addressing.
	podTrackers []*podTracker

	// Effective trackers that are assigned to this Activator.
	// This is a subset of podIPTrackers.
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
	logger *zap.SugaredLogger) *revisionThrottler {
	logger = logger.With(zap.Object(logkey.Key, logging.NamespacedName(revID)))
	var (
		revBreaker breaker
		lbp        lbPolicy
	)
	switch {
	case containerConcurrency == 0:
		revBreaker = newInfiniteBreaker(logger)
		lbp = randomChoice2Policy
	case containerConcurrency <= 3:
		// For very low CC values use first available pod.
		revBreaker = queue.NewBreaker(breakerParams)
		lbp = firstAvailableLBPolicy
	default:
		// Otherwise RR.
		revBreaker = queue.NewBreaker(breakerParams)
		lbp = newRoundRobinPolicy()
	}
	return &revisionThrottler{
		revID:                revID,
		containerConcurrency: containerConcurrency,
		breaker:              revBreaker,
		logger:               logger,
		protocol:             proto,
		activatorIndex:       *atomic.NewInt32(-1), // Start with unknown.
		lbPolicy:             lbp,
	}
}

func noop() {}

// Returns a dest that at the moment of choosing had an open slot
// for request.
func (rt *revisionThrottler) acquireDest(ctx context.Context) (func(), *podTracker) {
	rt.mux.RLock()
	defer rt.mux.RUnlock()

	if rt.clusterIPTracker != nil {
		return noop, rt.clusterIPTracker
	}
	return rt.lbPolicy(ctx, rt.assignedTrackers)
}

func (rt *revisionThrottler) try(ctx context.Context, function func(string) error) error {
	var ret error

	// Retrying infinitely as long as we receive no dest. Outer semaphore and inner
	// pod capacity are not changed atomically, hence they can race each other. We
	// "reenqueue" requests should that happen.
	reenqueue := true
	for reenqueue {
		reenqueue = false
		if err := rt.breaker.Maybe(ctx, func() {
			cb, tracker := rt.acquireDest(ctx)
			if tracker == nil {
				// This can happen if individual requests raced each other or if pod
				// capacity was decreased after passing the outer semaphore.
				reenqueue = true
				return
			}
			defer cb()
			// We already reserved a guaranteed spot. So just execute the passed functor.
			ret = function(tracker.dest)
		}); err != nil {
			return err
		}
	}
	return ret
}

func (rt *revisionThrottler) calculateCapacity(size, activatorCount int) int {
	targetCapacity := rt.containerConcurrency * size

	if size > 0 && (rt.containerConcurrency == 0 || targetCapacity > revisionMaxConcurrency) {
		// If cc==0, we need to pick a number, but it does not matter, since
		// infinite breaker will dole out as many tokens as it can.
		// For cc>0 we clamp targetCapacity to maxConcurrency because the backing
		// breaker requires some limit (it's backed by a chan struct{}), but the
		// limit is math.MaxInt32 so in practice this should never be a real limit.
		targetCapacity = revisionMaxConcurrency
	} else if targetCapacity > 0 {
		targetCapacity = minOneOrValue(targetCapacity / minOneOrValue(activatorCount))
	}

	return targetCapacity
}

// This makes sure we reset the capacity to the CC, since the pod
// might be reassigned to be exclusively used.
func (rt *revisionThrottler) resetTrackers() {
	if rt.containerConcurrency <= 0 {
		return
	}
	for _, t := range rt.podTrackers {
		// Reset to default.
		t.UpdateConcurrency(rt.containerConcurrency)
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
		// We do not have to process the `podTrackers` under lock, since
		// updateCapacity is guaranteed to be executed by a single goroutine.
		// But `assignedTrackers` is being read by the serving thread, so the
		// actual assignment has to be done under lock.

		// We're using cluster IP.
		if rt.clusterIPTracker != nil {
			return 0
		}

		// Sort, so we get more or less stable results.
		sort.Slice(rt.podTrackers, func(i, j int) bool {
			return rt.podTrackers[i].dest < rt.podTrackers[j].dest
		})
		assigned := rt.podTrackers
		if rt.containerConcurrency > 0 {
			rt.resetTrackers()
			assigned = assignSlice(rt.podTrackers, ai, ac, rt.containerConcurrency)
		}
		rt.logger.Debugf("Trackers %d/%d: assignment: %v", ai, ac, assigned)
		// The actual write out of the assigned trackers has to be under lock.
		rt.mux.Lock()
		defer rt.mux.Unlock()
		rt.assignedTrackers = assigned
		return len(assigned)
	}()

	capacity := 0
	if numTrackers > 0 {
		// Capacity is computed based off of number of trackers,
		// when using pod direct routing.
		capacity = rt.calculateCapacity(len(rt.podTrackers), ac)
	} else {
		// Capacity is computed off of number of ready backends,
		// when we are using clusterIP routing.
		capacity = rt.calculateCapacity(backendCount, ac)
	}
	rt.logger.Infof("Set capacity to %d (backends: %d, index: %d/%d)",
		capacity, backendCount, ai, ac)

	rt.backendCount = backendCount
	rt.breaker.UpdateConcurrency(capacity)
}

func (rt *revisionThrottler) updateThrottlerState(
	throttler *Throttler, backendCount int,
	trackers []*podTracker, clusterIPDest *podTracker) {
	rt.logger.Infof("Updating Revision Throttler with: clusterIP = %v, trackers = %d, backends = %d",
		clusterIPDest, len(trackers), backendCount)

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
		rt.updateCapacity(backendCount)
	} else {
		// If we do not have an address to target, then we should treat it
		// as though we have zero backends.
		rt.updateCapacity(0)
	}
}

// pickIndices picks the indices for the slicing.
func pickIndices(numTrackers, selfIndex, numActivators int) (beginIndex, endIndex, remnants int) {
	if numActivators > numTrackers {
		// 1. We have fewer pods than than activators. Assign the pods in round robin fashion.
		// With subsetting this is less of a problem and should almost never happen.
		// e.g. lt=3, #ac = 5; for selfIdx = 3 => 3 % 3 = 0, or for si = 5 => 5%3 = 2
		beginIndex = selfIndex % numTrackers
		endIndex = beginIndex + 1
		return
	}

	// 2. distribute equally and share the remnants
	// among all the activators, but with reduced capacity, if finite.
	sliceSize := numTrackers / numActivators
	remnants = numTrackers % numActivators
	beginIndex = selfIndex * sliceSize
	endIndex = beginIndex + sliceSize
	return
}

// assignSlice picks a subset of the individual pods to send requests to
// for this Activator instance. This only matters in case of direct
// to pod IP routing, and is irrelevant, when ClusterIP is used.
// assignSlice should receive podTrackers sorted by address.
func assignSlice(trackers []*podTracker, selfIndex, numActivators, cc int) []*podTracker {
	// When we're unassigned, doesn't matter what we return.
	lt := len(trackers)
	if selfIndex == -1 || lt <= 1 {
		return trackers
	}

	// If there's just a single activator. Take all the trackers.
	if numActivators == 1 {
		return trackers
	}

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
		// assignSlice is not called for CC=0 case.
		dcc := int(math.Ceil(float64(cc) / float64(numActivators)))
		// This is basically: x = append(x, trackers[len(trackers)-remnants:]...)
		// But we need to update the capacity.
		for _, t := range tail {
			t.UpdateConcurrency(dcc)
			x = append(x, t)
		}
	}
	return x
}

// This function will never be called in parallel but `try` can be called in parallel to this so we need
// to lock on updating concurrency / trackers
func (rt *revisionThrottler) handleUpdate(throttler *Throttler, update revisionDestsUpdate) {
	rt.logger.Debugw("Handling update",
		zap.String("ClusterIP", update.ClusterIPDest), zap.Object("dests", logging.StringSet(update.Dests)))

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
							QueueDepth:      breakerQueueDepth,
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
	revisionLister          servinglisters.RevisionLister
	serviceLister           corev1listers.ServiceLister
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
		serviceLister:      serviceinformer.Get(ctx).Lister(),
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
			AddFunc:    t.publicEndspointsUpdated,
			UpdateFunc: controller.PassNew(t.publicEndspointsUpdated),
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
func (t *Throttler) Try(ctx context.Context, function func(string) error) error {
	rt, err := t.getOrCreateRevisionThrottler(util.RevIDFrom(ctx))
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
			networking.ServicePortName(rev.GetProtocol()),
			queue.BreakerParams{QueueDepth: breakerQueueDepth, MaxConcurrency: revisionMaxConcurrency},
			t.logger,
		)
		t.revisionThrottlers[revID] = revThrottler
	}
	return revThrottler, nil
}

// revisionUpdated is used to ensure we have a backlog set up for a revision as soon as it is created
// rather than erroring with revision not found until a networking probe succeeds
func (t *Throttler) revisionUpdated(obj interface{}) {
	rev := obj.(*v1.Revision)
	revID := types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}

	t.logger.Debug("Revision update", zap.Object(logkey.Key, logging.NamespacedName(revID)))

	if _, err := t.getOrCreateRevisionThrottler(revID); err != nil {
		t.logger.Errorw("Failed to get revision throttler for revision",
			zap.Error(err), zap.Object(logkey.Key, logging.NamespacedName(revID)))
	}
}

// revisionDeleted is to clean up revision throttlers after a revision is deleted to prevent unbounded
// memory growth
func (t *Throttler) revisionDeleted(obj interface{}) {
	rev := obj.(*v1.Revision)
	revID := types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}

	t.logger.Debugw("Revision delete", zap.Object(logkey.Key, logging.NamespacedName(revID)))

	t.revisionThrottlersMutex.Lock()
	defer t.revisionThrottlersMutex.Unlock()
	delete(t.revisionThrottlers, revID)
}

func (t *Throttler) handleUpdate(update revisionDestsUpdate) {
	if rt, err := t.getOrCreateRevisionThrottler(update.Rev); err != nil {
		if k8serrors.IsNotFound(err) {
			t.logger.Debugw("Revision not found. It was probably removed",
				zap.Object(logkey.Key, logging.NamespacedName(update.Rev)))
		} else {
			t.logger.Errorw("Failed to get revision throttler", zap.Error(err),
				zap.Object(logkey.Key, logging.NamespacedName(update.Rev)))
		}
	} else {
		rt.handleUpdate(t, update)
	}
}

func (t *Throttler) handlePubEpsUpdate(eps *corev1.Endpoints) {
	t.logger.Infof("Public EPS updates: %#v", eps)

	revN := eps.Labels[serving.RevisionLabelKey]
	if revN == "" {
		// Perhaps, we're not the only ones using the same selector label.
		t.logger.Infof("Ignoring update for PublicService %s/%s", eps.Namespace, eps.Name)
		return
	}
	rev := types.NamespacedName{Name: revN, Namespace: eps.Namespace}
	if rt, err := t.getOrCreateRevisionThrottler(rev); err != nil {
		logger := t.logger.With(zap.Object(logkey.Key, logging.NamespacedName(rev)))
		if k8serrors.IsNotFound(err) {
			logger.Debug("Revision not found. It was probably removed")
		} else {
			logger.Errorw("Failed to get revision throttler", zap.Error(err))
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
	epsL := epSet.List()
	newNA, newAI := int32(len(epsL)), int32(inferIndex(epsL, selfIP))
	if newAI == -1 {
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
	rt.logger.Infof("This activator index is %d/%d was %d/%d",
		rt.activatorIndex, rt.numActivators, newAI, newNA)
	rt.updateCapacity(rt.backendCount)
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

func (t *Throttler) publicEndspointsUpdated(newObj interface{}) {
	endpoints := newObj.(*corev1.Endpoints)
	t.logger.Info("Updated public Endpoints: ", endpoints.Name)
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
	concurrency atomic.Int32

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
	return int(ib.concurrency.Load())
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
