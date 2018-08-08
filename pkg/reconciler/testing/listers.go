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
	"fmt"
	"reflect"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildlisters "github.com/knative/build/pkg/client/listers/build/v1alpha1"
	istiov1alpha3 "github.com/knative/pkg/apis/istio/v1alpha3"
	istiolisters "github.com/knative/pkg/client/listers/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

var kubeObjectTypes = []runtime.Object{
	&appsv1.Deployment{},
	&corev1.Service{},
	&corev1.Endpoints{},
	&corev1.ConfigMap{},
}

var buildObjectTypes = []runtime.Object{
	&buildv1alpha1.Build{},
}

var istioObjectTypes = []runtime.Object{
	&istiov1alpha3.VirtualService{},
}

var servingObjectTypes = []runtime.Object{
	&v1alpha1.Service{},
	&v1alpha1.Route{},
	&v1alpha1.Configuration{},
	&v1alpha1.Revision{},
}

type Listers struct {
	cache map[reflect.Type]cache.Indexer
}

func NewListers(objs []runtime.Object) Listers {
	cache := make(map[reflect.Type]cache.Indexer)

	var supportedObjects []runtime.Object
	supportedObjects = append(supportedObjects, kubeObjectTypes...)
	supportedObjects = append(supportedObjects, buildObjectTypes...)
	supportedObjects = append(supportedObjects, istioObjectTypes...)
	supportedObjects = append(supportedObjects, servingObjectTypes...)

	for _, t := range supportedObjects {
		cache[reflect.TypeOf(t).Elem()] = indexer()
	}

	ls := Listers{
		cache: cache,
	}

	for _, obj := range objs {
		t := reflect.TypeOf(obj).Elem()
		indexer, ok := cache[t]
		if !ok {
			panic(fmt.Sprintf("Unsupported type in TableTest %T", obj))
		}
		indexer.Add(obj)
	}

	return ls
}

func indexer() cache.Indexer {
	return cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
}

func (f *Listers) getObjects(types []runtime.Object) []runtime.Object {
	var objs []runtime.Object

	for _, t := range types {
		indexer := f.indexerForType(t)
		for _, item := range indexer.List() {
			objs = append(objs, item.(runtime.Object))
		}
	}
	return objs
}

func (f *Listers) indexerForType(obj runtime.Object) cache.Indexer {
	return f.cache[reflect.TypeOf(obj).Elem()]
}

func (f *Listers) GetKubeObjects() []runtime.Object {
	return f.getObjects(kubeObjectTypes)
}

func (f *Listers) GetBuildObjects() []runtime.Object {
	return f.getObjects(buildObjectTypes)
}

func (f *Listers) GetServingObjects() []runtime.Object {
	return f.getObjects(servingObjectTypes)
}

func (f *Listers) GetSharedObjects() []runtime.Object {
	return f.getObjects(istioObjectTypes)
}

func (f *Listers) GetServiceLister() servinglisters.ServiceLister {
	return servinglisters.NewServiceLister(f.indexerForType(&v1alpha1.Service{}))
}

func (f *Listers) GetRouteLister() servinglisters.RouteLister {
	return servinglisters.NewRouteLister(f.indexerForType(&v1alpha1.Route{}))
}

func (f *Listers) GetConfigurationLister() servinglisters.ConfigurationLister {
	return servinglisters.NewConfigurationLister(f.indexerForType(&v1alpha1.Configuration{}))
}

func (f *Listers) GetRevisionLister() servinglisters.RevisionLister {
	return servinglisters.NewRevisionLister(f.indexerForType(&v1alpha1.Revision{}))
}

func (f *Listers) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return istiolisters.NewVirtualServiceLister(f.indexerForType(&istiov1alpha3.VirtualService{}))
}

func (f *Listers) GetBuildLister() buildlisters.BuildLister {
	return buildlisters.NewBuildLister(f.indexerForType(&buildv1alpha1.Build{}))
}

func (f *Listers) GetDeploymentLister() appsv1listers.DeploymentLister {
	return appsv1listers.NewDeploymentLister(f.indexerForType(&appsv1.Deployment{}))
}

func (f *Listers) GetK8sServiceLister() corev1listers.ServiceLister {
	return corev1listers.NewServiceLister(f.indexerForType(&corev1.Service{}))
}

func (f *Listers) GetEndpointsLister() corev1listers.EndpointsLister {
	return corev1listers.NewEndpointsLister(f.indexerForType(&corev1.Endpoints{}))
}

func (f *Listers) GetConfigMapLister() corev1listers.ConfigMapLister {
	return corev1listers.NewConfigMapLister(f.indexerForType(&corev1.ConfigMap{}))
}
