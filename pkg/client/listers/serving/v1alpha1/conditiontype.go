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
	v1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ConditionTypeLister helps list ConditionTypes.
type ConditionTypeLister interface {
	// List lists all ConditionTypes in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.ConditionType, err error)
	// ConditionTypes returns an object that can list and get ConditionTypes.
	ConditionTypes(namespace string) ConditionTypeNamespaceLister
	ConditionTypeListerExpansion
}

// conditionTypeLister implements the ConditionTypeLister interface.
type conditionTypeLister struct {
	indexer cache.Indexer
}

// NewConditionTypeLister returns a new ConditionTypeLister.
func NewConditionTypeLister(indexer cache.Indexer) ConditionTypeLister {
	return &conditionTypeLister{indexer: indexer}
}

// List lists all ConditionTypes in the indexer.
func (s *conditionTypeLister) List(selector labels.Selector) (ret []*v1alpha1.ConditionType, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ConditionType))
	})
	return ret, err
}

// ConditionTypes returns an object that can list and get ConditionTypes.
func (s *conditionTypeLister) ConditionTypes(namespace string) ConditionTypeNamespaceLister {
	return conditionTypeNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ConditionTypeNamespaceLister helps list and get ConditionTypes.
type ConditionTypeNamespaceLister interface {
	// List lists all ConditionTypes in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.ConditionType, err error)
	// Get retrieves the ConditionType from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.ConditionType, error)
	ConditionTypeNamespaceListerExpansion
}

// conditionTypeNamespaceLister implements the ConditionTypeNamespaceLister
// interface.
type conditionTypeNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ConditionTypes in the indexer for a given namespace.
func (s conditionTypeNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.ConditionType, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ConditionType))
	})
	return ret, err
}

// Get retrieves the ConditionType from the indexer for a given namespace and name.
func (s conditionTypeNamespaceLister) Get(name string) (*v1alpha1.ConditionType, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("conditiontype"), name)
	}
	return obj.(*v1alpha1.ConditionType), nil
}
