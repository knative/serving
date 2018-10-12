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
	"testing"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

func TestSetupTriggers_NoTriggers(t *testing.T) {
	err := SetupTriggers("hello", &invocationRecorder{}, &invocationRecorder{}, &fakeInformerFactory{})

	if err != nil {
		t.Errorf("SetupTriggers should not have returned an error - got: %v", err)
	}
}

func TestSetupTriggers_NoInformer(t *testing.T) {
	fake := &fakeTriggers{
		triggers: []Trigger{{
			ObjectKind: schema.GroupVersionKind{Kind: "hello"},
		}},
	}

	errorInformerFactory := &fakeInformerFactory{err: errors.New("no informer")}
	err := SetupTriggers(fake, &invocationRecorder{}, &invocationRecorder{}, errorInformerFactory)

	if err == nil {
		t.Errorf("SetupTriggers should have returned an error")
	}
}

func TestSetupTriggers_FilteringAllow(t *testing.T) {
	triggers := &fakeTriggers{
		triggers: []Trigger{{
			ObjectKind:  schema.GroupVersionKind{Kind: "hello"},
			OwnerKind:   schema.GroupVersionKind{Kind: "hello-owner"},
			EnqueueType: EnqueueTracker,
		}},
	}

	recorder := &invocationRecorder{}
	informer := &fakeInformerFactory{}
	err := SetupTriggers(triggers, recorder, recorder, informer)

	if err != nil {
		t.Fatalf("unexpected error setting up triggers: %v", err)
	}

	owner := &metav1.ObjectMeta{
		Name: "hello-owner",
	}

	obj := &metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(owner, schema.GroupVersionKind{Kind: "hello-owner"}),
		},
	}

	informer.handler.OnAdd(obj)
	assertTrackerInvoked(t, recorder, "add")

	recorder.resetInvocations()
	informer.handler.OnUpdate(obj, obj)
	assertTrackerInvoked(t, recorder, "update")

	recorder.resetInvocations()

	informer.handler.OnDelete(obj)
	assertTrackerInvoked(t, recorder, "delete")
}

func TestSetupTriggers_FilteringBlocks(t *testing.T) {
	triggers := &fakeTriggers{
		triggers: []Trigger{{
			ObjectKind:  schema.GroupVersionKind{Kind: "object-kind"},
			OwnerKind:   schema.GroupVersionKind{Kind: "owner-kind"},
			EnqueueType: EnqueueTracker,
		}},
	}

	recorder := &invocationRecorder{}
	informer := &fakeInformerFactory{}
	err := SetupTriggers(triggers, recorder, recorder, informer)

	if err != nil {
		t.Fatalf("unexpected error setting up triggers: %v", err)
	}

	owner := &metav1.ObjectMeta{
		Name: "owner",
	}

	obj := &metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(owner, schema.GroupVersionKind{Kind: "different-owner-kind"}),
		},
	}

	informer.handler.OnAdd(obj)
	assertNoInvocations(t, recorder, "add")

	recorder.resetInvocations()

	informer.handler.OnUpdate(obj, obj)
	assertNoInvocations(t, recorder, "update")

	recorder.resetInvocations()

	informer.handler.OnDelete(obj)
	assertNoInvocations(t, recorder, "delete")
}

func TestSetupTriggers(t *testing.T) {
	gvk := schema.GroupVersionKind{Kind: "fun"}

	tests := []struct {
		name        string
		trigger     Trigger
		expectError bool
	}{{
		name: "enqueue object",
		trigger: Trigger{
			ObjectKind:  gvk,
			EnqueueType: EnqueueObject,
		},
	}, {
		name: "enqueue owner",
		trigger: Trigger{
			ObjectKind:  gvk,
			EnqueueType: EnqueueOwner,
		},
	}, {
		name: "enqueue tracker",
		trigger: Trigger{
			ObjectKind:  gvk,
			EnqueueType: EnqueueTracker,
		},
	}, {
		name:        "unknown enqueue type",
		expectError: true,
		trigger: Trigger{
			ObjectKind:  gvk,
			EnqueueType: "unknown type",
		},
	}, {
		name:        "empty object kind",
		expectError: true,
		trigger: Trigger{
			ObjectKind:  schema.EmptyObjectKind.GroupVersionKind(),
			EnqueueType: EnqueueObject,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			triggers := &fakeTriggers{
				triggers: []Trigger{test.trigger},
			}

			recorder := &invocationRecorder{}
			informer := &fakeInformerFactory{}
			err := SetupTriggers(triggers, recorder, recorder, informer)

			if err == nil && test.expectError {
				t.Errorf("SetupTriggers should have returned an error")
			} else if err != nil && test.expectError {
				return
			} else if err != nil {
				t.Errorf("SetupTriggers should not have returned an error - got: %v", err)
			}

			var assertCorrectInvocations func(*testing.T, *invocationRecorder, string)

			switch test.trigger.EnqueueType {
			case EnqueueObject:
				assertCorrectInvocations = assertEnqueueInvoked
			case EnqueueOwner:
				assertCorrectInvocations = assertEnqueueControllerOfInvoked
			case EnqueueTracker:
				assertCorrectInvocations = assertTrackerInvoked
			default:
				t.Fatalf("unexpected enqueue type: %v", test.trigger.EnqueueType)
			}

			informer.handler.OnAdd("add")
			assertCorrectInvocations(t, recorder, "add")

			recorder.resetInvocations()

			informer.handler.OnUpdate("update-old", "update-new")
			assertCorrectInvocations(t, recorder, "update")

			recorder.resetInvocations()

			informer.handler.OnDelete("delete")
			assertCorrectInvocations(t, recorder, "delete")
		})
	}
}

func assertNoInvocations(t *testing.T, r *invocationRecorder, event string) {
	t.Helper()

	if r.enqueueInvoked {
		t.Errorf("the controller's 'Enqueue' was unexpectedly setup correct for %q events", event)
	}

	if r.enqueueControllerOfInvoked {
		t.Errorf("the controller's 'EnqueueControllerOf' was unexpectedly setup for %q events", event)
	}

	if r.trackerInvoked {
		t.Errorf("the trackers's 'OnChange' was unexpectedly setup for %q events", event)
	}
}

func assertTrackerInvoked(t *testing.T, r *invocationRecorder, event string) {
	t.Helper()

	if r.enqueueInvoked {
		t.Errorf("the controller's 'Enqueue' was unexpectedly setup correct for %q events", event)
	}

	if r.enqueueControllerOfInvoked {
		t.Errorf("the controller's 'EnqueueControllerOf' was unexpectedly setup for %q events", event)
	}

	if !r.trackerInvoked {
		t.Errorf("the trackers's 'OnChange' was not setup for %q events", event)
	}
}

func assertEnqueueControllerOfInvoked(t *testing.T, r *invocationRecorder, event string) {
	t.Helper()

	if r.enqueueInvoked {
		t.Errorf("the controller's 'Enqueue' was unexpectedly setup correct for %q events", event)
	}

	if !r.enqueueControllerOfInvoked {
		t.Errorf("the controller's 'EnqueueControllerOf' was not setup for %q events", event)
	}

	if r.trackerInvoked {
		t.Errorf("the trackers's 'OnChange' was unexpectedly setup for %q events", event)
	}
}

func assertEnqueueInvoked(t *testing.T, r *invocationRecorder, event string) {
	t.Helper()

	if !r.enqueueInvoked {
		t.Errorf("the controller's 'Enqueue' was not setup for %q events", event)
	}

	if r.enqueueControllerOfInvoked {
		t.Errorf("the controller's 'EnqueueControllerOf' was unexpectedly setup for %q events", event)
	}

	if r.trackerInvoked {
		t.Errorf("the trackers's 'OnChange' was unexpectedly setup for %q events", event)
	}
}

type fakeTriggers struct {
	triggers []Trigger
}

func (f *fakeTriggers) Triggers() []Trigger {
	return f.triggers
}

type fakeInformerFactory struct {
	cache.SharedIndexInformer
	err     error
	handler cache.ResourceEventHandler
}

func (f *fakeInformerFactory) InformerFor(schema.GroupVersionKind) (cache.SharedIndexInformer, error) {
	return f, f.err
}

func (f *fakeInformerFactory) AddEventHandler(handler cache.ResourceEventHandler) {
	f.handler = handler
}

type invocationRecorder struct {
	enqueueInvoked             bool
	enqueueControllerOfInvoked bool
	trackerInvoked             bool
}

func (f *invocationRecorder) resetInvocations() {
	f.enqueueControllerOfInvoked = false
	f.enqueueInvoked = false
	f.trackerInvoked = false
}

func (f *invocationRecorder) Enqueue(obj interface{}) {
	f.enqueueInvoked = true
}

func (f *invocationRecorder) EnqueueKey(key string) {
	panic("EnqueueKey should not be invoked")
}

func (f *invocationRecorder) EnqueueControllerOf(obj interface{}) {
	f.enqueueControllerOfInvoked = true
}

func (f *invocationRecorder) OnChanged(obj interface{}) {
	f.trackerInvoked = true
}
