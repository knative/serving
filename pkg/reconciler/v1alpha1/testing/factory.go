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
	"testing"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamicclientset "k8s.io/client-go/dynamic/fake"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/scale"
	fakescaleclient "k8s.io/client-go/scale/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	fakecachingclientset "github.com/knative/caching/pkg/client/clientset/versioned/fake"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler"
)

const (
	// maxEventBufferSize is the estimated max number of event notifications that
	// can be buffered during reconciliation.
	maxEventBufferSize = 10
)

// Ctor functions create a k8s controller with given params.
type Ctor func(*Listers, reconciler.Options) controller.Reconciler

// scaleClient returns a fake scale client that will serve a single provided object.
// Kubernetes does not come currently with a normal fake, as other APIs, so we did this...
func scaleClient(f ktesting.Fake, objects ...runtime.Object) scale.ScalesGetter {
	var scaleObj *autoscalingv1.Scale
	for _, obj := range objects {
		if so, ok := obj.(*autoscalingv1.Scale); ok {
			scaleObj = so
			break
		}
	}
	scaleClient := &fakescaleclient.FakeScaleClient{f}
	scaleClient.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, scaleObj, nil
	})
	return scaleClient
}

// MakeFactory creates a reconciler factory with fake clients and controller created by `ctor`.
func MakeFactory(ctor Ctor) Factory {
	return func(t *testing.T, r *TableRow) (controller.Reconciler, ActionRecorderList, EventList, *FakeStatsReporter) {
		ls := NewListers(r.Objects)

		kubeClient := fakekubeclientset.NewSimpleClientset(ls.GetKubeObjects()...)
		sharedClient := fakesharedclientset.NewSimpleClientset(ls.GetSharedObjects()...)
		client := fakeclientset.NewSimpleClientset(ls.GetServingObjects()...)

		dynamicClient := fakedynamicclientset.NewSimpleDynamicClient(runtime.NewScheme(), ls.GetBuildObjects()...)
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
			ScaleClientSet:   scaleClient(client.Fake, ls.GetKubeObjects()...),
			Recorder:         eventRecorder,
			StatsReporter:    statsReporter,
			Logger:           TestLogger(t),
		})

		for _, reactor := range r.WithReactors {
			kubeClient.PrependReactor("*", "*", reactor)
			sharedClient.PrependReactor("*", "*", reactor)
			client.PrependReactor("*", "*", reactor)
			dynamicClient.PrependReactor("*", "*", reactor)
			cachingClient.PrependReactor("*", "*", reactor)
		}

		// Validate all Create operations through the serving client.
		client.PrependReactor("create", "*", ValidateCreates)
		client.PrependReactor("update", "*", ValidateUpdates)

		actionRecorderList := ActionRecorderList{sharedClient, dynamicClient, client, kubeClient, cachingClient}
		eventList := EventList{Recorder: eventRecorder}

		return c, actionRecorderList, eventList, statsReporter
	}
}
