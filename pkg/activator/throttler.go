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

	"go.uber.org/zap"

	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/resources"

	corev1 "k8s.io/api/core/v1"
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
	breakersMux sync.Mutex
	breakers    map[RevisionID]*queue.Breaker

	breakerParams   queue.BreakerParams
	logger          *zap.SugaredLogger
	endpointsLister corev1listers.EndpointsLister
	revisionLister  servinglisters.RevisionLister
	sksLister       netlisters.ServerlessServiceLister
}

// NewThrottler creates a new Throttler.
func NewThrottler(
	params queue.BreakerParams,
	endpointsInformer corev1informers.EndpointsInformer,
	sksLister netlisters.ServerlessServiceLister,
	revisionLister servinglisters.RevisionLister,
	logger *zap.SugaredLogger) *Throttler {

	throttler := &Throttler{
		breakers:        make(map[RevisionID]*queue.Breaker),
		breakerParams:   params,
		logger:          logger,
		endpointsLister: endpointsInformer.Lister(),
		revisionLister:  revisionLister,
		sksLister:       sksLister,
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

	return throttler
}

// Remove deletes the breaker from the bookkeeping.
func (t *Throttler) Remove(rev RevisionID) {
	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()
	delete(t.breakers, rev)
}

// UpdateCapacity updates the max concurrency of the Breaker corresponding to a revision.
func (t *Throttler) UpdateCapacity(rev RevisionID, size int) error {
	revision, err := t.revisionLister.Revisions(rev.Namespace).Get(rev.Name)
	if err != nil {
		return err
	}
	breaker, _ := t.getOrCreateBreaker(rev)
	return t.updateCapacity(breaker, int(revision.Spec.ContainerConcurrency), size)
}

// Try potentially registers a new breaker in our bookkeeping
// and executes the `function` on the Breaker.
// It returns an error if either breaker doesn't have enough capacity,
// or breaker's registration didn't succeed, e.g. getting endpoints or update capacity failed.
func (t *Throttler) Try(rev RevisionID, function func()) error {
	breaker, existed := t.getOrCreateBreaker(rev)
	if !existed {
		// Need to fetch the latest endpoints state, in case we missed the update.
		if err := t.forceUpdateCapacity(rev, breaker); err != nil {
			return err
		}
	}
	if !breaker.Maybe(function) {
		return ErrActivatorOverload
	}
	return nil
}

// This method updates Breaker's concurrency.
func (t *Throttler) updateCapacity(breaker *queue.Breaker, cc, size int) (err error) {
	targetCapacity := cc * size
	if size > 0 && (cc == 0 || targetCapacity > t.breakerParams.MaxConcurrency) {
		// The concurrency is unlimited, thus hand out as many tokens as we can in this breaker.
		targetCapacity = t.breakerParams.MaxConcurrency
	}

	return breaker.UpdateConcurrency(targetCapacity)
}

// getOrCreateBreaker retrieves existing breaker or creates a new one.
// This is important for not loosing the update signals
// that came before the requests reached the Activator's Handler.
func (t *Throttler) getOrCreateBreaker(rev RevisionID) (*queue.Breaker, bool) {
	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()
	breaker, ok := t.breakers[rev]
	if !ok {
		breaker = queue.NewBreaker(t.breakerParams)
		t.breakers[rev] = breaker
	}
	return breaker, ok
}

// forceUpdateCapacity fetches the endpoints and updates the capacity of the newly created breaker.
// This avoids a potential deadlock in case if we missed the updates from the Endpoints informer.
// This could happen because of a restart of the Activator or when a new one is added as part of scale out.
func (t *Throttler) forceUpdateCapacity(rev RevisionID, breaker *queue.Breaker) (err error) {
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
	size, err := resources.FetchReadyAddressCount(t.endpointsLister, sks.Namespace, sks.Status.PrivateServiceName)
	if err != nil {
		return err
	}
	return t.updateCapacity(breaker, int(revision.Spec.ContainerConcurrency), size)
}

// endpointsUpdated is a handler function to be used by the Endpoints informer.
// It updates the endpoints in the Throttler if the number of hosts changed and
// the revision already exists (we don't want to create/update throttlers for the endpoints
// that do not belong to any revision).
//
// This function must not be called in parallel to not induce a wrong order of events.
func (t *Throttler) endpointsUpdated(newObj interface{}) {
	endpoints := newObj.(*corev1.Endpoints)
	addresses := resources.ReadyAddressCount(endpoints)
	revID := RevisionID{endpoints.Namespace, resources.ParentResourceFromService(endpoints.Name)}
	if err := t.UpdateCapacity(revID, addresses); err != nil {
		t.logger.With(zap.String(logkey.Key, revID.String())).Errorw("updating capacity failed", zap.Error(err))
	}
}

// endpointsDeleted is a handler function to be used by the Endpoints informer.
// It removes the Breaker from the Throttler bookkeeping.
func (t *Throttler) endpointsDeleted(obj interface{}) {
	ep := obj.(*corev1.Endpoints)
	name := resources.ParentResourceFromService(ep.Name)
	revID := RevisionID{ep.Namespace, name}
	t.Remove(revID)
}
