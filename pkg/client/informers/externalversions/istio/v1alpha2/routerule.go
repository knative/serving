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
package v1alpha2

import (
	time "time"

	istio_v1alpha2 "github.com/knative/serving/pkg/apis/istio/v1alpha2"
	versioned "github.com/knative/serving/pkg/client/clientset/versioned"
	internalinterfaces "github.com/knative/serving/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha2 "github.com/knative/serving/pkg/client/listers/istio/v1alpha2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// RouteRuleInformer provides access to a shared informer and lister for
// RouteRules.
type RouteRuleInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha2.RouteRuleLister
}

type routeRuleInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewRouteRuleInformer constructs a new informer for RouteRule type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewRouteRuleInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredRouteRuleInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredRouteRuleInformer constructs a new informer for RouteRule type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredRouteRuleInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ConfigV1alpha2().RouteRules(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ConfigV1alpha2().RouteRules(namespace).Watch(options)
			},
		},
		&istio_v1alpha2.RouteRule{},
		resyncPeriod,
		indexers,
	)
}

func (f *routeRuleInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredRouteRuleInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *routeRuleInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&istio_v1alpha2.RouteRule{}, f.defaultInformer)
}

func (f *routeRuleInformer) Lister() v1alpha2.RouteRuleLister {
	return v1alpha2.NewRouteRuleLister(f.Informer().GetIndexer())
}
