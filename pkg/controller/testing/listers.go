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

package testing

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
)

// ServiceLister is a lister.ServiceLister fake for testing.
type ServiceLister struct {
	Err   error
	Items []*v1alpha1.Service
}

// Assert that our fake implements the interface it is faking.
var _ listers.ServiceLister = (*ServiceLister)(nil)

func (r *ServiceLister) List(selector labels.Selector) (ret []*v1alpha1.Service, err error) {
	return r.Items, r.Err
}

func (r *ServiceLister) Services(namespace string) listers.ServiceNamespaceLister {
	return &nsServiceLister{r: r, ns: namespace}
}

type nsServiceLister struct {
	r  *ServiceLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ listers.ServiceNamespaceLister = (*nsServiceLister)(nil)

func (r *nsServiceLister) List(selector labels.Selector) (ret []*v1alpha1.Service, err error) {
	return r.r.Items, r.r.Err
}

func (r *nsServiceLister) Get(name string) (*v1alpha1.Service, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// RouteLister is a lister.RouteLister fake for testing.
type RouteLister struct {
	Err   error
	Items []*v1alpha1.Route
}

// Assert that our fake implements the interface it is faking.
var _ listers.RouteLister = (*RouteLister)(nil)

func (r *RouteLister) List(selector labels.Selector) (ret []*v1alpha1.Route, err error) {
	return r.Items, r.Err
}

func (r *RouteLister) Routes(namespace string) listers.RouteNamespaceLister {
	return &nsRouteLister{r: r, ns: namespace}
}

type nsRouteLister struct {
	r  *RouteLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ listers.RouteNamespaceLister = (*nsRouteLister)(nil)

func (r *nsRouteLister) List(selector labels.Selector) (ret []*v1alpha1.Route, err error) {
	return r.r.Items, r.r.Err
}

func (r *nsRouteLister) Get(name string) (*v1alpha1.Route, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// ConfigurationLister is a lister.ConfigurationLister fake for testing.
type ConfigurationLister struct {
	Err   error
	Items []*v1alpha1.Configuration
}

// Assert that our fake implements the interface it is faking.
var _ listers.ConfigurationLister = (*ConfigurationLister)(nil)

func (r *ConfigurationLister) List(selector labels.Selector) (ret []*v1alpha1.Configuration, err error) {
	return r.Items, r.Err
}

func (r *ConfigurationLister) Configurations(namespace string) listers.ConfigurationNamespaceLister {
	return &nsConfigurationLister{r: r, ns: namespace}
}

type nsConfigurationLister struct {
	r  *ConfigurationLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ listers.ConfigurationNamespaceLister = (*nsConfigurationLister)(nil)

func (r *nsConfigurationLister) List(selector labels.Selector) (ret []*v1alpha1.Configuration, err error) {
	return r.r.Items, r.r.Err
}

func (r *nsConfigurationLister) Get(name string) (*v1alpha1.Configuration, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// RevisionLister is a lister.RevisionLister fake for testing.
type RevisionLister struct {
	Err   error
	Items []*v1alpha1.Revision
}

// Assert that our fake implements the interface it is faking.
var _ listers.RevisionLister = (*RevisionLister)(nil)

func (r *RevisionLister) List(selector labels.Selector) (ret []*v1alpha1.Revision, err error) {
	return r.Items, r.Err
}

func (r *RevisionLister) Revisions(namespace string) listers.RevisionNamespaceLister {
	return &nsRevisionLister{r: r, ns: namespace}
}

type nsRevisionLister struct {
	r  *RevisionLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ listers.RevisionNamespaceLister = (*nsRevisionLister)(nil)

func (r *nsRevisionLister) List(selector labels.Selector) (ret []*v1alpha1.Revision, err error) {
	return r.r.Items, r.r.Err
}

func (r *nsRevisionLister) Get(name string) (*v1alpha1.Revision, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}
