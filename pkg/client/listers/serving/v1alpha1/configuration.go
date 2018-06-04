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
package v1alpha1

import (
	v1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ConfigurationLister helps list Configurations.
type ConfigurationLister interface {
	// List lists all Configurations in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.Configuration, err error)
	// Configurations returns an object that can list and get Configurations.
	Configurations(namespace string) ConfigurationNamespaceLister
	ConfigurationListerExpansion
}

// configurationLister implements the ConfigurationLister interface.
type configurationLister struct {
	indexer cache.Indexer
}

// NewConfigurationLister returns a new ConfigurationLister.
func NewConfigurationLister(indexer cache.Indexer) ConfigurationLister {
	return &configurationLister{indexer: indexer}
}

// List lists all Configurations in the indexer.
func (s *configurationLister) List(selector labels.Selector) (ret []*v1alpha1.Configuration, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Configuration))
	})
	return ret, err
}

// Configurations returns an object that can list and get Configurations.
func (s *configurationLister) Configurations(namespace string) ConfigurationNamespaceLister {
	return configurationNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ConfigurationNamespaceLister helps list and get Configurations.
type ConfigurationNamespaceLister interface {
	// List lists all Configurations in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.Configuration, err error)
	// Get retrieves the Configuration from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.Configuration, error)
	ConfigurationNamespaceListerExpansion
}

// configurationNamespaceLister implements the ConfigurationNamespaceLister
// interface.
type configurationNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Configurations in the indexer for a given namespace.
func (s configurationNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.Configuration, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Configuration))
	})
	return ret, err
}

// Get retrieves the Configuration from the indexer for a given namespace and name.
func (s configurationNamespaceLister) Get(name string) (*v1alpha1.Configuration, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("configuration"), name)
	}
	return obj.(*v1alpha1.Configuration), nil
}
