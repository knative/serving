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
	"errors"
	"sync"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/reconciler"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

const (
	capacityUpdateFailure = "updating capacity failed"
	// OverloadMessage indicates that throttler has no free slots to buffer the request.
	OverloadMessage = "activator overload"
)

// ThrottlerParams defines the parameters of the Throttler.
type ThrottlerParams struct {
	BreakerParams queue.BreakerParams
	Logger        *zap.SugaredLogger
	GetEndpoints  func(RevisionID) (int32, error)
	GetRevision   func(RevisionID) (*v1alpha1.Revision, error)
}

// NewThrottler creates a new Throttler.
func NewThrottler(params ThrottlerParams) *Throttler {
	breakers := make(map[RevisionID]*queue.Breaker)
	return &Throttler{breakers: breakers, breakerParams: params.BreakerParams, logger: params.Logger, getEndpoints: params.GetEndpoints, getRevision: params.GetRevision}
}

// Throttler keeps the mapping of Revisions to Breakers
// and allows updating max concurrency dynamically of respective Breakers.
// Max concurrency is essentially the number of semaphore tokens the Breaker has in rotation.
// The manipulation of the parameter is done via `UpdateCapacity()` method.
// It enables the use case to start with max concurrency set to 0 (no requests are sent because no endpoints are available)
// and gradually increase its value depending on the external condition (e.g. new endpoints become available)
type Throttler struct {
	breakers      map[RevisionID]*queue.Breaker
	breakerParams queue.BreakerParams
	logger        *zap.SugaredLogger
	getEndpoints  func(RevisionID) (int32, error)
	getRevision   func(RevisionID) (*v1alpha1.Revision, error)
	mux           sync.Mutex
}

// Remove deletes the breaker from the bookkeeping.
func (t *Throttler) Remove(rev RevisionID) {
	t.mux.Lock()
	defer t.mux.Unlock()
	delete(t.breakers, rev)
}

// UpdateCapacity updates the max concurrency of the Breaker corresponding to a revision.
func (t *Throttler) UpdateCapacity(rev RevisionID, size int32) error {
	revision, err := t.getRevision(rev)
	if err != nil {
		return err
	}
	breaker, _ := t.getOrCreateBreaker(rev)
	err = t.updateCapacity(revision, breaker, size)
	return err
}

// Try potentially registers a new breaker in our bookkeeping
// and executes the `function` on the Breaker.
// It returns an error if either breaker doesn't have enough capacity,
// or breaker's registration didn't succeed, e.g. getting endpoints or update capacity failed.
func (t *Throttler) Try(rev RevisionID, function func()) error {
	breaker, existed := t.getOrCreateBreaker(rev)
	if !existed {
		if err := t.forceUpdateCapacity(rev, breaker); err != nil {
			return err
		}
	}
	if ok := breaker.Maybe(function); !ok {
		return errors.New(OverloadMessage)
	}
	return nil
}

// This method updates Breaker's concurrency and requires external synchronization, `updateCapacity` requires `mux` to be held.
func (t *Throttler) updateCapacity(revision *v1alpha1.Revision, breaker *queue.Breaker, size int32) (err error) {
	cc := int32(revision.Spec.ContainerConcurrency)
	// The concurrency is unlimited, thus hand out as many tokens as we can in this breaker.
	if cc == 0 {
		cc = t.breakerParams.MaxConcurrency
	}
	delta := size*cc - breaker.Capacity()
	// Do not update throttler's concurrency if the same number of endpoints is received
	// or the number is negative.
	if delta <= 0 {
		return nil
	}
	if err := breaker.UpdateConcurrency(delta); err != nil {
		return err
	}
	return nil
}

// getOrCreateBreaker retrieves existing breaker or creates a new one.
// This is important for not loosing the update signals
// that came before the requests reached the Activator's Handler.
func (t *Throttler) getOrCreateBreaker(rev RevisionID) (*queue.Breaker, bool) {
	t.mux.Lock()
	defer t.mux.Unlock()
	breaker, ok := t.breakers[rev]
	if !ok {
		breaker = queue.NewBreaker(t.breakerParams)
		t.breakers[rev] = breaker
	}
	return breaker, ok
}

// forceUpdateCapacity fetches the endpoints and update the capacity of the newly created breaker.
// This avoids a potential deadlock in case if we missed the updates from the Endpoints informer.
// This could happen because of a restart of the Activator or when a new one is added as part of scale out.
func (t *Throttler) forceUpdateCapacity(rev RevisionID, breaker *queue.Breaker) (err error) {
	size, err := t.getEndpoints(rev)
	if err != nil {
		return err
	}
	revision, err := t.getRevision(rev)
	if err != nil {
		return err
	}
	err = t.updateCapacity(revision, breaker, size)
	if err != nil {
		return err
	}
	return nil
}

// UpdateEndpoints is a handler function to be used by the Endpoints informer.
// It updates the endpoints in the Throttler if the number of hosts changed and
// the revision already exists (we don't want to create/update throttlers for the endpoints
// that do not belong to any revision).
func UpdateEndpoints(throttler *Throttler) func(oldObj, newObj interface{}) {
	return func(oldObj, newObj interface{}) {
		endpoints := newObj.(*corev1.Endpoints)
		addresses := EndpointsAddressCount(endpoints.Subsets)
		revID := RevisionID{endpoints.Namespace, reconciler.GetServingRevisionNameForK8sService(endpoints.Name)}
		_, err := throttler.getRevision(revID)
		if err != nil {
			throttler.logger.Errorw(capacityUpdateFailure, zap.Error(err))
			return
		}
		if err = throttler.UpdateCapacity(revID, int32(addresses)); err != nil {
			throttler.logger.Errorw(capacityUpdateFailure, zap.Error(err))
		}
	}
}

// DeleteBreaker is a handler function to be used by the Endpoints informer.
// It removes the Breaker from the Throttler bookkeeping.
func DeleteBreaker(throttler *Throttler) func(obj interface{}) {
	return func(obj interface{}) {
		rev := obj.(*v1alpha1.Revision)
		revID := RevisionID{rev.Namespace, rev.Name}
		throttler.Remove(revID)
	}
}

// EndpointsAddressCount returns the total number of addresses registered for the endpoint.
func EndpointsAddressCount(subsets []corev1.EndpointSubset) int {
	var total int
	for _, subset := range subsets {
		total += len(subset.Addresses)
	}
	return total
}
