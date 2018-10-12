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

package v1alpha1

import (
	"fmt"
	"reflect"
	"time"

	cachingclientset "github.com/knative/caching/pkg/client/clientset/versioned"
	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions"
	pkgapis "github.com/knative/pkg/apis"
	servingclientset "github.com/knative/serving/pkg/client/clientset/versioned"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type DependencyFactory struct {
	*reconciler.DependencyFactory

	Serving struct {
		Client          servingclientset.Interface
		InformerFactory servinginformers.SharedInformerFactory
	}

	Caching struct {
		Client          cachingclientset.Interface
		InformerFactory cachinginformers.SharedInformerFactory
	}
}

func NewDependencyFactory(cfg *rest.Config, resync time.Duration) (*DependencyFactory, error) {
	common, err := reconciler.NewDependencyFactory(cfg, resync)

	if err != nil {
		return nil, err
	}

	d := &DependencyFactory{DependencyFactory: common}

	d.Serving.Client, err = servingclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("Error building serving clientset: %v", err)
	}

	d.Caching.Client, err = cachingclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("Error building caching clientset: %v", err)
	}

	d.Serving.InformerFactory = servinginformers.NewSharedInformerFactory(
		d.Serving.Client,
		resync,
	)

	d.Caching.InformerFactory = cachinginformers.NewSharedInformerFactory(
		d.Caching.Client,
		resync,
	)

	return d, nil
}

func (d *DependencyFactory) StartInformers(stopCh <-chan struct{}) {
	d.DependencyFactory.StartInformers(stopCh)

	d.Serving.InformerFactory.Start(stopCh)
	d.Caching.InformerFactory.Start(stopCh)
}

func (d *DependencyFactory) WaitForInformerCacheSync(stopCh <-chan struct{}) error {
	if err := d.DependencyFactory.WaitForInformerCacheSync(stopCh); err != nil {
		return err
	}

	waiters := []func(stopCh <-chan struct{}) map[reflect.Type]bool{
		d.Serving.InformerFactory.WaitForCacheSync,
		d.Caching.InformerFactory.WaitForCacheSync,
	}

	for _, wait := range waiters {
		result := wait(stopCh)

		for informerType, started := range result {
			if !started {
				return fmt.Errorf("failed to wait for cache sync for type %q", informerType.Name())
			}
		}
	}

	return nil
}

func (d *DependencyFactory) InformerFor(gvk schema.GroupVersionKind) (cache.SharedIndexInformer, error) {
	if informer, err := d.DependencyFactory.InformerFor(gvk); informer != nil && err == nil {
		return informer, nil
	}

	gvr := pkgapis.KindToResource(gvk)

	if i, err := d.Serving.InformerFactory.ForResource(gvr); i != nil && err == nil {
		return i.Informer(), nil
	}

	if i, err := d.Caching.InformerFactory.ForResource(gvr); i != nil && err == nil {
		return i.Informer(), nil
	}

	return nil, fmt.Errorf("Unabled to find informer for resource %q", gvr)
}
