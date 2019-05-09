/*
Copyright 2018 The Knative Authors.

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

package testing

import (
	"context"
	"encoding/json"
	"testing"

	logtesting "github.com/knative/pkg/logging/testing"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamicclientset "k8s.io/client-go/dynamic/fake"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	fakecachingclientset "github.com/knative/caching/pkg/client/clientset/versioned/fake"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler"

	. "github.com/knative/pkg/reconciler/testing"
)

const (
	// maxEventBufferSize is the estimated max number of event notifications that
	// can be buffered during reconciliation.
	maxEventBufferSize = 10
)

// Ctor functions create a k8s controller with given params.
type Ctor func(*Listers, reconciler.Options) controller.Reconciler

// MakeFactory creates a reconciler factory with fake clients and controller created by `ctor`.
func MakeFactory(ctor Ctor) Factory {
	return func(t *testing.T, r *TableRow) (controller.Reconciler, ActionRecorderList, EventList, *FakeStatsReporter) {
		ls := NewListers(r.Objects)

		kubeClient := fakekubeclientset.NewSimpleClientset(ls.GetKubeObjects()...)
		sharedClient := fakesharedclientset.NewSimpleClientset(ls.GetSharedObjects()...)
		client := fakeclientset.NewSimpleClientset(ls.GetServingObjects()...)
		dynamicClient := fakedynamicclientset.NewSimpleDynamicClient(
			NewScheme(), ToUnstructured(t, r.Objects)...)

		// The dynamic client's support for patching is BS.  Implement it
		// here via PrependReactor (this can be overridden below by the
		// provided reactors).
		dynamicClient.PrependReactor("patch", "*",
			func(action ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, nil
			})

		cachingClient := fakecachingclientset.NewSimpleClientset(ls.GetCachingObjects()...)
		eventRecorder := record.NewFakeRecorder(maxEventBufferSize)
		statsReporter := &FakeStatsReporter{}

		PrependGenerateNameReactor(&client.Fake)
		PrependGenerateNameReactor(&dynamicClient.Fake)

		// Set up our Controller from the fakes.
		c := ctor(&ls, reconciler.Options{
			KubeClientSet:    kubeClient,
			SharedClientSet:  sharedClient,
			DynamicClientSet: dynamicClient,
			CachingClientSet: cachingClient,
			ServingClientSet: client,
			Recorder:         eventRecorder,
			StatsReporter:    statsReporter,
			Logger:           logtesting.TestLogger(t),
		})

		for _, reactor := range r.WithReactors {
			kubeClient.PrependReactor("*", "*", reactor)
			sharedClient.PrependReactor("*", "*", reactor)
			client.PrependReactor("*", "*", reactor)
			dynamicClient.PrependReactor("*", "*", reactor)
			cachingClient.PrependReactor("*", "*", reactor)
		}

		// Validate all Create operations through the serving client.
		client.PrependReactor("create", "*", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
			// TODO(n3wscott): context.Background is the best we can do at the moment, but it should be set-able.
			return ValidateCreates(context.Background(), action)
		})
		client.PrependReactor("update", "*", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
			// TODO(n3wscott): context.Background is the best we can do at the moment, but it should be set-able.
			return ValidateUpdates(context.Background(), action)
		})

		actionRecorderList := ActionRecorderList{sharedClient, dynamicClient, client, kubeClient, cachingClient}
		eventList := EventList{Recorder: eventRecorder}

		return c, actionRecorderList, eventList, statsReporter
	}
}

// ToUnstructured takes a list of k8s resources and converts them to
// Unstructured objects.
// We must pass objects as Unstructured to the dynamic client fake, or it
// won't handle them properly.
func ToUnstructured(t *testing.T, objs []runtime.Object) (us []runtime.Object) {
	sch := NewScheme()
	for _, obj := range objs {
		obj = obj.DeepCopyObject() // Don't mess with the primary copy
		// Determine and set the TypeMeta for this object based on our test scheme.
		gvks, _, err := sch.ObjectKinds(obj)
		if err != nil {
			t.Fatalf("Unable to determine kind for type: %v", err)
		}
		apiv, k := gvks[0].ToAPIVersionAndKind()
		ta, err := meta.TypeAccessor(obj)
		if err != nil {
			t.Fatalf("Unable to create type accessor: %v", err)
		}
		ta.SetAPIVersion(apiv)
		ta.SetKind(k)

		b, err := json.Marshal(obj)
		if err != nil {
			t.Fatalf("Unable to marshal: %v", err)
		}
		u := &unstructured.Unstructured{}
		if err := json.Unmarshal(b, u); err != nil {
			t.Fatalf("Unable to unmarshal: %v", err)
		}
		us = append(us, u)
	}
	return
}
