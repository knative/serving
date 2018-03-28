/*
Copyright 2018 Google LLC

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
	v1alpha2 "github.com/elafros/elafros/pkg/apis/istio/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// RouteRuleLister helps list RouteRules.
type RouteRuleLister interface {
	// List lists all RouteRules in the indexer.
	List(selector labels.Selector) (ret []*v1alpha2.RouteRule, err error)
	// RouteRules returns an object that can list and get RouteRules.
	RouteRules(namespace string) RouteRuleNamespaceLister
	RouteRuleListerExpansion
}

// routeRuleLister implements the RouteRuleLister interface.
type routeRuleLister struct {
	indexer cache.Indexer
}

// NewRouteRuleLister returns a new RouteRuleLister.
func NewRouteRuleLister(indexer cache.Indexer) RouteRuleLister {
	return &routeRuleLister{indexer: indexer}
}

// List lists all RouteRules in the indexer.
func (s *routeRuleLister) List(selector labels.Selector) (ret []*v1alpha2.RouteRule, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha2.RouteRule))
	})
	return ret, err
}

// RouteRules returns an object that can list and get RouteRules.
func (s *routeRuleLister) RouteRules(namespace string) RouteRuleNamespaceLister {
	return routeRuleNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// RouteRuleNamespaceLister helps list and get RouteRules.
type RouteRuleNamespaceLister interface {
	// List lists all RouteRules in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha2.RouteRule, err error)
	// Get retrieves the RouteRule from the indexer for a given namespace and name.
	Get(name string) (*v1alpha2.RouteRule, error)
	RouteRuleNamespaceListerExpansion
}

// routeRuleNamespaceLister implements the RouteRuleNamespaceLister
// interface.
type routeRuleNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all RouteRules in the indexer for a given namespace.
func (s routeRuleNamespaceLister) List(selector labels.Selector) (ret []*v1alpha2.RouteRule, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha2.RouteRule))
	})
	return ret, err
}

// Get retrieves the RouteRule from the indexer for a given namespace and name.
func (s routeRuleNamespaceLister) Get(name string) (*v1alpha2.RouteRule, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha2.Resource("routerule"), name)
	}
	return obj.(*v1alpha2.RouteRule), nil
}
