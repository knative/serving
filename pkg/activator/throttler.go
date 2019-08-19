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
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	netlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/resources"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ErrActivatorOverload indicates that throttler has no free slots to buffer the request.
var ErrActivatorOverload = errors.New("activator overload")

// Throttler keeps the mapping of Revisions to Breakers
// and allows updating max concurrency dynamically of respective Breakers.
// Max concurrency is essentially the number of semaphore tokens the Breaker has in rotation.
// The manipulation of the parameter is done via `UpdateCapacity()` method.
// It enables the use case to start with max concurrency set to 0 (no requests are sent because no endpoints are available)
// and gradually increase its value depending on the external condition (e.g. new endpoints become available)
type Throttler struct {
	breakersMux sync.RWMutex
	breakers    map[RevisionID]breaker

	breakerParams   queue.BreakerParams
	logger          *zap.SugaredLogger
	endpointsLister corev1listers.EndpointsLister
	revisionLister  servinglisters.RevisionLister
	sksLister       netlisters.ServerlessServiceLister

	pcache           *probeCache
	numActivatorsMux sync.RWMutex
	numActivators    int
}

type breaker interface {
	Capacity() int
	Maybe(ctx context.Context, thunk func()) bool
	UpdateConcurrency(int) error
}

// NewThrottler creates a new Throttler.
func NewThrottler(
	params queue.BreakerParams,
	endpointsInformer corev1informers.EndpointsInformer,
	sksLister netlisters.ServerlessServiceLister,
	revisionLister servinglisters.RevisionLister,
	logger *zap.SugaredLogger) *Throttler {

	throttler := &Throttler{
		breakers:        make(map[RevisionID]breaker),
		breakerParams:   params,
		logger:          logger,
		endpointsLister: endpointsInformer.Lister(),
		revisionLister:  revisionLister,
		sksLister:       sksLister,
		pcache:          newProbeCache(),
	}

	// Update/create the breaker in the throttler when the number of endpoints changes.
	// Pass only the endpoints created by revisions.
	// TODO(greghaynes) we have to allow unset and use the old RevisionUID filter for backwards compat.
	// When we can assume our ServiceTypeKey label is present in all services we can filter all but
	// networking.ServiceTypeKey == networking.ServiceTypePublic
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.LabelExistsFilterFunc(serving.RevisionUID),
			// We are only interested in the private services, since that is
			// what is populated by the actual revision backends.
			reconciler.LabelFilterFunc(networking.ServiceTypeKey, string(networking.ServiceTypePrivate), true),
		),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    throttler.endpointsUpdated,
			UpdateFunc: controller.PassNew(throttler.endpointsUpdated),
			DeleteFunc: throttler.endpointsDeleted,
		},
	})

	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.NameFilterFunc(K8sServiceName),
			reconciler.NamespaceFilterFunc(system.Namespace()),
		),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    throttler.activatorEndpointsUpdated,
			UpdateFunc: controller.PassNew(throttler.activatorEndpointsUpdated),
		},
	})

	return throttler
}

// Remove deletes the breaker from the bookkeeping.
func (t *Throttler) Remove(rev RevisionID) {
	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()
	delete(t.breakers, rev)
}

// ShouldProbe returns true if we should probe.
func (t *Throttler) ShouldProbe(revID RevisionID) bool {
	return t.pcache.should(revID)
}

// MarkProbe marks revision as successfully probed.
func (t *Throttler) MarkProbe(revID RevisionID) {
	t.pcache.mark(revID)
}

// UpdateCapacity updates the max concurrency of the Breaker corresponding to a revision.
func (t *Throttler) UpdateCapacity(rev RevisionID, size int) error {
	// If we have no backends -- make sure to always probe.
	if size == 0 {
		t.pcache.unmark(rev)
	}
	revision, err := t.revisionLister.Revisions(rev.Namespace).Get(rev.Name)
	if err != nil {
		return err
	}
	breaker, _, err := t.getOrCreateBreaker(rev)
	if err != nil {
		return err
	}
	return t.updateCapacity(breaker, int(revision.Spec.GetContainerConcurrency()), size, t.activatorCount())
}

// Try potentially registers a new breaker in our bookkeeping
// and executes the `function` on the Breaker.
// It returns an error if either breaker doesn't have enough capacity,
// or breaker's registration didn't succeed, e.g. getting endpoints or update capacity failed.
func (t *Throttler) Try(ctx context.Context, rev RevisionID, function func()) error {
	breaker, existed, err := t.getOrCreateBreaker(rev)
	if err != nil {
		return err
	}
	if !existed {
		// Need to fetch the latest endpoints state, in case we missed the update.
		if err := t.forceUpdateCapacity(rev, breaker, t.activatorCount()); err != nil {
			return err
		}
	}
	if !breaker.Maybe(ctx, function) {
		return ErrActivatorOverload
	}
	return nil
}

func (t *Throttler) activatorCount() int {
	t.numActivatorsMux.RLock()
	defer t.numActivatorsMux.RUnlock()
	return t.numActivators
}

func (t *Throttler) activatorEndpointsUpdated(newObj interface{}) {
	endpoints := newObj.(*corev1.Endpoints)

	t.numActivatorsMux.Lock()
	defer t.numActivatorsMux.Unlock()
	t.numActivators = resources.ReadyAddressCount(endpoints)
	t.updateAllBreakerCapacity(t.numActivators)
}

// minOneOrValue function returns num if its greater than 1
// else the function returns 1
func minOneOrValue(num int) int {
	if num > 1 {
		return num
	}
	return 1
}

// This method updates Breaker's concurrency.
func (t *Throttler) updateCapacity(breaker breaker, cc, size, activatorCount int) (err error) {
	targetCapacity := cc * size

	if size > 0 && (cc == 0 || targetCapacity > t.breakerParams.MaxConcurrency) {
		// If cc==0, we need to pick a number, but it does not matter, since
		// infinite breaker will dole out as many tokens as it can.
		targetCapacity = t.breakerParams.MaxConcurrency
	} else if targetCapacity > 0 {
		targetCapacity = minOneOrValue(targetCapacity / minOneOrValue(activatorCount))
	}
	return breaker.UpdateConcurrency(targetCapacity)
}

// getOrCreateBreaker retrieves existing breaker or creates a new one.
// This is important for not losing the update signals that came before the requests reached
// the Activator's Handler.
// The lock handling is optimized via https://en.wikipedia.org/wiki/Double-checked_locking.
func (t *Throttler) getOrCreateBreaker(revID RevisionID) (breaker, bool, error) {
	t.breakersMux.RLock()
	breaker, ok := t.breakers[revID]
	t.breakersMux.RUnlock()
	if ok {
		return breaker, true, nil
	}

	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()

	breaker, ok = t.breakers[revID]
	if ok {
		return breaker, true, nil
	}

	revision, err := t.revisionLister.Revisions(revID.Namespace).Get(revID.Name)
	if err != nil {
		return nil, false, err
	}
	if revision.Spec.GetContainerConcurrency() == 0 {
		breaker = &infiniteBreaker{
			broadcast: make(chan struct{}),
		}
	} else {
		breaker = queue.NewBreaker(t.breakerParams)
	}
	t.breakers[revID] = breaker

	return breaker, false, nil
}

// forceUpdateCapacity fetches the endpoints and updates the capacity of the newly created breaker.
// This avoids a potential deadlock in case if we missed the updates from the Endpoints informer.
// This could happen because of a restart of the Activator or when a new one is added as part of scale out.
func (t *Throttler) forceUpdateCapacity(rev RevisionID, breaker breaker, activatorCount int) (err error) {
	revision, err := t.revisionLister.Revisions(rev.Namespace).Get(rev.Name)
	if err != nil {
		return err
	}

	// SKS name matches revision name.
	sks, err := t.sksLister.ServerlessServices(rev.Namespace).Get(rev.Name)
	if err != nil {
		return err
	}

	// We have to read the private service endpoints in activator
	// in order to count the serving pod count, since the public one
	// may point at ourselves.
	podCounter := resources.NewScopedEndpointsCounter(t.endpointsLister, sks.Namespace, sks.Status.PrivateServiceName)
	size, err := podCounter.ReadyCount()
	if err != nil {
		return err
	}

	return t.updateCapacity(breaker, int(revision.Spec.GetContainerConcurrency()), size, activatorCount)
}

// updateAllBreakerCapacity updates the capacity of all breakers.
func (t *Throttler) updateAllBreakerCapacity(activatorCount int) {
	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()
	for revID, breaker := range t.breakers {
		if err := t.forceUpdateCapacity(revID, breaker, activatorCount); err != nil {
			t.logger.With(zap.String(logkey.Key, revID.String())).Errorw("updating capacity failed", zap.Error(err))
		}
	}
}

// endpointsUpdated is a handler function to be used by the Endpoints informer.
// It updates the endpoints in the Throttler if the number of hosts changed and
// the revision already exists (we don't want to create/update throttlers for the endpoints
// that do not belong to any revision).
//
// This function must not be called in parallel to not induce a wrong order of events.
func (t *Throttler) endpointsUpdated(newObj interface{}) {
	ep := newObj.(*corev1.Endpoints)

	revisionName, ok := ep.Labels[serving.RevisionLabelKey]
	if !ok {
		t.logger.Errorf("updating capacity failed: endpoints %s/%s didn't have a revision label", ep.Namespace, ep.Name)
		return
	}
	addresses := resources.ReadyAddressCount(ep)
	revID := RevisionID{ep.Namespace, revisionName}
	if err := t.UpdateCapacity(revID, addresses); err != nil {
		t.logger.With(zap.String(logkey.Key, revID.String())).Errorw("updating capacity failed", zap.Error(err))
	}
}

// endpointsDeleted is a handler function to be used by the Endpoints informer.
// It removes the Breaker from the Throttler bookkeeping.
func (t *Throttler) endpointsDeleted(obj interface{}) {
	ep := obj.(*corev1.Endpoints)

	revisionName, ok := ep.Labels[serving.RevisionLabelKey]
	if !ok {
		t.logger.Errorf("deleting breaker failed: endpoints %s/%s didn't have a revision label", ep.Namespace, ep.Name)
		return
	}
	revID := RevisionID{ep.Namespace, revisionName}
	t.Remove(revID)
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
}

func (ib *infiniteBreaker) Capacity() int {
	return int(atomic.LoadInt32(&ib.concurrency))
}

func zeroOrOne(x int) int32 {
	if x == 0 {
		return 0
	}
	return 1
}

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

func (ib *infiniteBreaker) Maybe(ctx context.Context, thunk func()) bool {
	has := ib.Capacity()
	// We're scaled to serve.
	if has > 0 {
		thunk()
		return true
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
		return true
	case <-ctx.Done():
		return false
	}
}

type probeCache struct {
	mu     sync.RWMutex
	probes sets.String
}

func newProbeCache() *probeCache {
	return &probeCache{
		probes: sets.NewString(),
	}
}

// should returns true if we should probe the given revision.
func (pc *probeCache) should(revID RevisionID) bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return !pc.probes.Has(revID.String())
}

// mark marks the revision as been probed.
func (pc *probeCache) mark(revID RevisionID) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.probes.Insert(revID.String())
}

// unmark removes the probe cache entry for the revision.
func (pc *probeCache) unmark(revID RevisionID) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.probes.Delete(revID.String())
}
