/*
Copyright 2018 The Knative Authors

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

package reconciler

import (
	"fmt"

	"github.com/knative/pkg/controller"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

const (
	// EnqueueObject indicates that the object kind being informed on should
	// queued objects for reconciliation.
	EnqueueObject EnqueueType = "controller-object"

	// EnqueueOwner indicates that the object kind being informed on should
	// queue it's controlling owner for reconciliation.
	EnqueueOwner EnqueueType = "controller-owner"

	// EnqueueTracker indicates that the object kind being informed on should
	// queue objects to the tracker.
	EnqueueTracker EnqueueType = "tracker-object"
)

type (
	// EnqueueType is an enum who's value specifies what should be
	// enqueued and where.
	EnqueueType string

	// Trigger allows a phase or reconciler to specify which object changes
	// should trigger reconciliation.
	Trigger struct {
		// ObjectKind defines the object that should trigger reconcilation
		// +required
		ObjectKind schema.GroupVersionKind

		// OwnerKind defines the kind of controlling owner reference the
		// ObjectKind should have
		// +optional
		OwnerKind schema.GroupVersionKind

		// EnqueueType specifies which object should be enqueued
		// +required
		EnqueueType EnqueueType
	}

	// WithTriggers defines an interface a reconciler or phase may conform to
	// that indicates the objects changes that should trigger reconciliation.
	WithTriggers interface {
		Triggers() []Trigger
	}
)

type (
	// informerFactory is a consumer interface type for SetupTriggers
	informerFactory interface {
		InformerFor(schema.GroupVersionKind) (cache.SharedIndexInformer, error)
	}

	// tracker is a consumer interface type for SetupTriggers
	objectTracker interface {
		OnChanged(obj interface{})
	}
)

// SetupTriggers will setup the correct informers and callbacks if the
// provided object conforms to WithTriggers interface.
//
// It returns no error if the object does not conform to the WithTriggers interface.
func SetupTriggers(obj interface{}, queue WorkQueue, tracker objectTracker, factory informerFactory) error {
	holder, ok := obj.(WithTriggers)

	if !ok {
		return nil
	}

	for _, trigger := range holder.Triggers() {

		if trigger.ObjectKind.Empty() {
			return errors.New("trigger ObjectKind must not be empty")
		}

		informer, err := factory.InformerFor(trigger.ObjectKind)
		if err != nil {
			return err
		}

		var enqueue func(obj interface{})

		switch trigger.EnqueueType {
		case EnqueueObject:
			enqueue = queue.Enqueue
			break
		case EnqueueOwner:
			enqueue = queue.EnqueueControllerOf
		case EnqueueTracker:
			enqueue = tracker.OnChanged
		default:
			return fmt.Errorf("unknown trigger enqueue type %q", trigger.EnqueueType)
		}

		var handler cache.ResourceEventHandler = cache.ResourceEventHandlerFuncs{
			AddFunc:    enqueue,
			UpdateFunc: controller.PassNew(enqueue),
			DeleteFunc: enqueue,
		}

		if !trigger.OwnerKind.Empty() {
			handler = cache.FilteringResourceEventHandler{
				FilterFunc: controller.Filter(trigger.OwnerKind),
				Handler:    handler,
			}
		}

		informer.AddEventHandler(handler)
	}

	return nil
}
