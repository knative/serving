/*
Copyright 2022 The Knative Authors

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

package watch

import (
	"context"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"go.uber.org/atomic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"

	"knative.dev/serving/test"
)

type (
	Event struct {
		Type   watch.EventType
		Object *unstructured.Unstructured
	}
	ObjectEvents map[string][]Event
	GVRHistory   map[schema.GroupVersionResource]ObjectEvents
)

type resourceWatcher struct {
	t       *testing.T
	stopped *atomic.Bool
	watches []watch.Interface
	history GVRHistory
}

func StartCapture(t *testing.T, clients *test.Clients) func() GVRHistory {
	ctx := context.Background()

	resources := []schema.GroupVersionResource{
		servingv1.SchemeGroupVersion.WithResource("services"),
		servingv1.SchemeGroupVersion.WithResource("configurations"),
		servingv1.SchemeGroupVersion.WithResource("revisions"),
		servingv1.SchemeGroupVersion.WithResource("routes"),

		netv1alpha1.SchemeGroupVersion.WithResource("serverlessservices"),
		netv1alpha1.SchemeGroupVersion.WithResource("ingresses"),
		netv1alpha1.SchemeGroupVersion.WithResource("certificates"),

		autoscalingv1alpha1.SchemeGroupVersion.WithResource("metrics"),
		autoscalingv1alpha1.SchemeGroupVersion.WithResource("podautoscalers"),
	}

	watcher := resourceWatcher{
		t:       t,
		stopped: atomic.NewBool(false),
		history: make(GVRHistory),
	}

	for _, r := range resources {
		watch, err := clients.Dynamic.Resource(r).Watch(ctx, metav1.ListOptions{})

		if err != nil {
			t.Fatal("failed to create watch", err)
		}

		watcher.StartCapture(r, watch)
	}

	return func() GVRHistory {
		watcher.stopped.Store(true)
		for _, w := range watcher.watches {
			w.Stop()
		}
		return watcher.history
	}
}

func (r *resourceWatcher) StartCapture(gvr schema.GroupVersionResource, w watch.Interface) {
	r.watches = append(r.watches, w)

	events, ok := r.history[gvr]
	if !ok {
		events = make(ObjectEvents)
		r.history[gvr] = events
	}

	go func() {
		for e := range w.ResultChan() {
			if r.stopped.Load() {
				return
			}
			switch e.Type {
			case watch.Bookmark:
				r.t.Log(spew.Sprintf("Watch boookmark %#+v", e.Object))

			case watch.Error:
				errObject := apierrors.FromObject(e.Object)
				statusErr, ok := errObject.(*apierrors.StatusError)
				if !ok {
					r.t.Log(spew.Sprintf("Received an error which is not *metav1.Status but %#+v", e.Object))
					return
				}
				status := statusErr.ErrStatus
				r.t.Log(spew.Sprintf("Received an error %#+v", status))

			case watch.Added, watch.Deleted, watch.Modified:
				obj := e.Object.(*unstructured.Unstructured)
				key := obj.GetNamespace() + "/" + obj.GetName()
				events[key] = append(events[key], Event{Type: e.Type, Object: obj})
			default:
				r.t.Log("Unidentified watch type ", e.Type)
				return
			}
		}
	}()
}
