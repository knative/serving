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

package testing

import (
	"reflect"

	cachingclientset "github.com/knative/caching/pkg/client/clientset/versioned"
	fakecachingclientset "github.com/knative/caching/pkg/client/clientset/versioned/fake"
	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions"
	"github.com/knative/pkg/apis"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	servingclientset "github.com/knative/serving/pkg/client/clientset/versioned"
	fakeservingclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/testing"
	"github.com/knative/serving/pkg/reconciler/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamicclientset "k8s.io/client-go/dynamic/fake"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

type newReconcilerFunc func(reconciler.CommonOptions, *v1alpha1.DependencyFactory) reconciler.Reconciler

func ReconcilerSetup(newReconciler newReconcilerFunc) testing.ReconcilerSetupFunc {
	return func(opts reconciler.CommonOptions, objs []runtime.Object) (reconciler.Reconciler, []FakeClient) {
		deps := NewFakeDependencies(objs)
		reconciler := newReconciler(opts, deps)
		return reconciler, fakeClients(deps)
	}
}

func PhaseSetup(newPhase interface{}) testing.PhaseSetupFunc {
	return func(opts reconciler.CommonOptions, objs []runtime.Object) (interface{}, []FakeClient) {
		deps := NewFakeDependencies(objs)

		// TODO(dprotaso) add validation to produce better error messages
		constructor := reflect.ValueOf(newPhase)

		in := []reflect.Value{
			reflect.ValueOf(opts),
			reflect.ValueOf(deps),
		}

		out := constructor.Call(in)

		return out[0].Interface(), fakeClients(deps)
	}
}

func fakeClients(deps *v1alpha1.DependencyFactory) []testing.FakeClient {
	return []testing.FakeClient{
		deps.Kubernetes.Client.(*fakekubeclientset.Clientset),
		deps.Shared.Client.(*fakesharedclientset.Clientset),
		deps.Serving.Client.(*fakeservingclientset.Clientset),
		deps.Caching.Client.(*fakecachingclientset.Clientset),
		deps.Dynamic.Client.(*fakedynamicclientset.FakeDynamicClient),
	}
}

func NewFakeDependencies(objs []runtime.Object) *v1alpha1.DependencyFactory {
	scheme, sorter, commonDeps := testing.NewFakeDependencies(objs,
		fakecachingclientset.AddToScheme,
		fakeservingclientset.AddToScheme,
	)

	servingObjs := sorter.ObjectsForSchemeFunc(fakeservingclientset.AddToScheme)
	cachingObjs := sorter.ObjectsForSchemeFunc(fakecachingclientset.AddToScheme)

	fakeServingClientset := fakeservingclientset.NewSimpleClientset(servingObjs...)
	fakeCachingClientset := fakecachingclientset.NewSimpleClientset(cachingObjs...)

	servingInformer := servinginformers.NewSharedInformerFactory(fakeServingClientset, 0)
	cachingInformer := cachinginformers.NewSharedInformerFactory(fakeCachingClientset, 0)

	for _, obj := range objs {
		kinds, _, _ := scheme.ObjectKinds(obj)
		for _, kind := range kinds {
			resource := apis.KindToResource(kind)
			if inf, _ := servingInformer.ForResource(resource); inf != nil {
				inf.Informer().GetStore().Add(obj)
			}
			if inf, _ := cachingInformer.ForResource(resource); inf != nil {
				inf.Informer().GetStore().Add(obj)
			}
		}
	}

	df := &v1alpha1.DependencyFactory{
		DependencyFactory: commonDeps,
		Serving: struct {
			Client          servingclientset.Interface
			InformerFactory servinginformers.SharedInformerFactory
		}{
			Client:          fakeServingClientset,
			InformerFactory: servingInformer,
		},

		Caching: struct {
			Client          cachingclientset.Interface
			InformerFactory cachinginformers.SharedInformerFactory
		}{
			Client:          fakeCachingClientset,
			InformerFactory: cachingInformer,
		},
	}

	return df
}
