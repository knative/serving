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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	WatchEvent struct {
		ReceiveTime time.Time
		Type        watch.EventType
		Object      *unstructured.Unstructured
	}
	ObjectHistory struct {
		K8sEvents   []*unstructured.Unstructured
		WatchEvents []WatchEvent
	}
	// Maps namespace/name to an object's history
	ObjectHistories map[string]*ObjectHistory
	GVRHistory      map[schema.GroupVersionResource]ObjectHistories
)

type resourceWatcher struct {
	t       *testing.T
	watches []watch.Interface
	done    []chan struct{}
	history GVRHistory
	objLock sync.RWMutex
}

var resources = map[schema.GroupVersionKind]schema.GroupVersionResource{
	servingv1.SchemeGroupVersion.WithKind("Service"):       servingv1.SchemeGroupVersion.WithResource("services"),
	servingv1.SchemeGroupVersion.WithKind("Configuration"): servingv1.SchemeGroupVersion.WithResource("configurations"),
	servingv1.SchemeGroupVersion.WithKind("Revision"):      servingv1.SchemeGroupVersion.WithResource("revisions"),
	servingv1.SchemeGroupVersion.WithKind("Route"):         servingv1.SchemeGroupVersion.WithResource("routes"),

	netv1alpha1.SchemeGroupVersion.WithKind("ServerlessService"): netv1alpha1.SchemeGroupVersion.WithResource("serverlessservices"),
	netv1alpha1.SchemeGroupVersion.WithKind("Ingress"):           netv1alpha1.SchemeGroupVersion.WithResource("ingresses"),
	netv1alpha1.SchemeGroupVersion.WithKind("Certificate"):       netv1alpha1.SchemeGroupVersion.WithResource("certificates"),

	autoscalingv1alpha1.SchemeGroupVersion.WithKind("Metric"):        autoscalingv1alpha1.SchemeGroupVersion.WithResource("metrics"),
	autoscalingv1alpha1.SchemeGroupVersion.WithKind("PodAutoscaler"): autoscalingv1alpha1.SchemeGroupVersion.WithResource("podautoscalers"),

	appsv1.SchemeGroupVersion.WithKind("Deployment"): appsv1.SchemeGroupVersion.WithResource("deployments"),
	appsv1.SchemeGroupVersion.WithKind("ReplicaSet"): appsv1.SchemeGroupVersion.WithResource("replicasets"),
	corev1.SchemeGroupVersion.WithKind("Pod"):        corev1.SchemeGroupVersion.WithResource("pods"),
}

func StartCapture(t *testing.T, clients *test.Clients) func() GVRHistory {
	ctx := context.Background()

	watcher := resourceWatcher{
		t:       t,
		history: make(GVRHistory, len(resources)),
	}

	for _, r := range resources {
		watcher.history[r] = make(ObjectHistories)
		watch, err := clients.Dynamic.Resource(r).Watch(ctx, metav1.ListOptions{})

		if err != nil {
			t.Fatal("failed to create watch", err)
		}

		watcher.startCapture(r, watch)
	}

	if err := watcher.startEventCapture(clients, resources); err != nil {
		t.Fatal("failed to create watch", err)
	}

	return func() GVRHistory {
		for _, w := range watcher.watches {
			w.Stop()
		}
		for _, done := range watcher.done {
			<-done
		}
		return watcher.history
	}
}

func (r *resourceWatcher) startEventCapture(clients *test.Clients, resources map[schema.GroupVersionKind]schema.GroupVersionResource) error {
	eventGVR := corev1.SchemeGroupVersion.WithResource("events")

	w, err := clients.Dynamic.Resource(eventGVR).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	done := make(chan struct{})

	r.watches = append(r.watches, w)
	r.done = append(r.done, done)

	go func() {
		defer close(done)
		for e := range w.ResultChan() {
			switch e.Type {
			case watch.Bookmark:
				r.t.Log(spew.Sprintf("Watch boookmark %#+v", e.Object))
			case watch.Error:
				r.handleWatchError(e)
			case watch.Added, watch.Deleted, watch.Modified:
				obj := e.Object.(*unstructured.Unstructured)

				apiVersion, _, _ := unstructured.NestedString(obj.Object, "involvedObject", "apiVersion")
				kind, _, _ := unstructured.NestedString(obj.Object, "involvedObject", "kind")

				gvk := schema.FromAPIVersionAndKind(apiVersion, kind)
				gvr, ok := resources[gvk]
				if !ok {
					continue
				}
				events := r.history[gvr]
				name, _, _ := unstructured.NestedString(obj.Object, "involvedObject", "name")
				namespace, _, _ := unstructured.NestedString(obj.Object, "involvedObject", "namespace")

				key := namespace + "/" + name
				r.objLock.Lock()
				history, ok := events[key]
				if !ok {
					history = new(ObjectHistory)
					events[key] = history
				}
				r.objLock.Unlock()
				history.K8sEvents = append(history.K8sEvents, obj)
			default:
				r.t.Log("Unidentified watch type ", e.Type)
				return
			}
		}
	}()

	return nil
}

func (r *resourceWatcher) startCapture(gvr schema.GroupVersionResource, w watch.Interface) {
	done := make(chan struct{})

	r.watches = append(r.watches, w)
	r.done = append(r.done, done)
	events := r.history[gvr]

	go func() {
		defer close(done)
		for e := range w.ResultChan() {
			switch e.Type {
			case watch.Bookmark:
				r.t.Log(spew.Sprintf("Watch boookmark %#+v", e.Object))

			case watch.Error:
				r.handleWatchError(e)
			case watch.Added, watch.Deleted, watch.Modified:
				obj := e.Object.(*unstructured.Unstructured)

				key := obj.GetNamespace() + "/" + obj.GetName()
				r.objLock.Lock()
				history, ok := events[key]
				if !ok {
					history = new(ObjectHistory)
					events[key] = history
				}
				r.objLock.Unlock()
				history.WatchEvents = append(history.WatchEvents, WatchEvent{Type: e.Type, Object: obj, ReceiveTime: time.Now().UTC()})
			default:
				r.t.Log("Unidentified watch type ", e.Type)
				return
			}
		}
	}()
}

func (r *resourceWatcher) handleWatchError(e watch.Event) {
	errObject := apierrors.FromObject(e.Object)
	statusErr, ok := errObject.(*apierrors.StatusError)
	if !ok {
		r.t.Log(spew.Sprintf("Received an error which is not *metav1.Status but %#+v", e.Object))
		return
	}
	status := statusErr.ErrStatus

	if strings.Contains(status.Message, "response body closed") {
		return
	}
	r.t.Log(spew.Sprintf("Received an error %#+v", status))
}
