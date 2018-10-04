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

	fakecachingclientset "github.com/knative/caching/pkg/client/clientset/versioned/fake"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamicclientset "k8s.io/client-go/dynamic/fake"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

type Ctor func(*Listers, reconciler.Options) controller.Reconciler

func MakeFactory(ctor Ctor) Factory {
	return func(t *testing.T, r *TableRow) (controller.Reconciler, ActionRecorderList) {
		ls := NewListers(r.Objects)

		kubeClient := fakekubeclientset.NewSimpleClientset(ls.GetKubeObjects()...)
		sharedClient := fakesharedclientset.NewSimpleClientset(ls.GetSharedObjects()...)
		client := fakeclientset.NewSimpleClientset(ls.GetServingObjects()...)
		dynamicClient := fakedynamicclientset.NewSimpleDynamicClient(runtime.NewScheme(), ls.GetBuildObjects()...)
		cachingClient := fakecachingclientset.NewSimpleClientset(ls.GetCachingObjects()...)

		// Set up our Controller from the fakes.
		c := ctor(&ls, reconciler.Options{
			KubeClientSet:    kubeClient,
			SharedClientSet:  sharedClient,
			DynamicClientSet: dynamicClient,
			CachingClientSet: cachingClient,
			ServingClientSet: client,
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

		return c, ActionRecorderList{sharedClient, dynamicClient, client, kubeClient, cachingClient}
	}
}
