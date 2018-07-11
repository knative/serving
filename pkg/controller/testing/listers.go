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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildlisters "github.com/knative/build/pkg/client/listers/build/v1alpha1"
	istiov1alpha3 "github.com/knative/serving/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	istiolisters "github.com/knative/serving/pkg/client/listers/istio/v1alpha3"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// ServiceLister is a lister.ServiceLister fake for testing.
type ServiceLister struct {
	Err   error
	Items []*v1alpha1.Service
}

// Assert that our fake implements the interface it is faking.
var _ listers.ServiceLister = (*ServiceLister)(nil)

func (r *ServiceLister) List(selector labels.Selector) (results []*v1alpha1.Service, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
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

func (r *nsServiceLister) List(selector labels.Selector) (results []*v1alpha1.Service, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
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

func (r *RouteLister) List(selector labels.Selector) (results []*v1alpha1.Route, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
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

func (r *nsRouteLister) List(selector labels.Selector) (results []*v1alpha1.Route, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
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

func (r *ConfigurationLister) List(selector labels.Selector) (results []*v1alpha1.Configuration, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
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

func (r *nsConfigurationLister) List(selector labels.Selector) (results []*v1alpha1.Configuration, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
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

func (r *RevisionLister) List(selector labels.Selector) (results []*v1alpha1.Revision, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
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

func (r *nsRevisionLister) List(selector labels.Selector) (results []*v1alpha1.Revision, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsRevisionLister) Get(name string) (*v1alpha1.Revision, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// BuildLister is a lister.BuildLister fake for testing.
type BuildLister struct {
	Err   error
	Items []*buildv1alpha1.Build
}

// Assert that our fake implements the interface it is faking.
var _ buildlisters.BuildLister = (*BuildLister)(nil)

func (r *BuildLister) List(selector labels.Selector) (results []*buildv1alpha1.Build, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
}

func (r *BuildLister) Builds(namespace string) buildlisters.BuildNamespaceLister {
	return &nsBuildLister{r: r, ns: namespace}
}

type nsBuildLister struct {
	r  *BuildLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ buildlisters.BuildNamespaceLister = (*nsBuildLister)(nil)

func (r *nsBuildLister) List(selector labels.Selector) (results []*buildv1alpha1.Build, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsBuildLister) Get(name string) (*buildv1alpha1.Build, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// DeploymentLister is a lister.DeploymentLister fake for testing.
type DeploymentLister struct {
	Err   error
	Items []*appsv1.Deployment
}

// Assert that our fake implements the interface it is faking.
var _ appsv1listers.DeploymentLister = (*DeploymentLister)(nil)

func (r *DeploymentLister) List(selector labels.Selector) (results []*appsv1.Deployment, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
}

func (r *DeploymentLister) Deployments(namespace string) appsv1listers.DeploymentNamespaceLister {
	return &nsDeploymentLister{r: r, ns: namespace}
}

func (r *DeploymentLister) GetDeploymentsForReplicaSet(rs *appsv1.ReplicaSet) (results []*appsv1.Deployment, err error) {
	return nil, fmt.Errorf("unimplemented GetDeploymentsForReplicaSet(%v)", rs)
}

type nsDeploymentLister struct {
	r  *DeploymentLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ appsv1listers.DeploymentNamespaceLister = (*nsDeploymentLister)(nil)

func (r *nsDeploymentLister) List(selector labels.Selector) (results []*appsv1.Deployment, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsDeploymentLister) Get(name string) (*appsv1.Deployment, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// EndpointsLister is a lister.EndpointsLister fake for testing.
type EndpointsLister struct {
	Err   error
	Items []*corev1.Endpoints
}

// Assert that our fake implements the interface it is faking.
var _ corev1listers.EndpointsLister = (*EndpointsLister)(nil)

func (r *EndpointsLister) List(selector labels.Selector) (results []*corev1.Endpoints, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
}

func (r *EndpointsLister) Endpoints(namespace string) corev1listers.EndpointsNamespaceLister {
	return &nsEndpointsLister{r: r, ns: namespace}
}

type nsEndpointsLister struct {
	r  *EndpointsLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ corev1listers.EndpointsNamespaceLister = (*nsEndpointsLister)(nil)

func (r *nsEndpointsLister) List(selector labels.Selector) (results []*corev1.Endpoints, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsEndpointsLister) Get(name string) (*corev1.Endpoints, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// K8sServiceLister is a lister.ServiceLister fake for testing.
type K8sServiceLister struct {
	Err   error
	Items []*corev1.Service
}

// Assert that our fake implements the interface it is faking.
var _ corev1listers.ServiceLister = (*K8sServiceLister)(nil)

func (r *K8sServiceLister) List(selector labels.Selector) (results []*corev1.Service, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
}

func (r *K8sServiceLister) Services(namespace string) corev1listers.ServiceNamespaceLister {
	return &nsK8sServiceLister{r: r, ns: namespace}
}

func (r *K8sServiceLister) GetPodServices(p *corev1.Pod) (results []*corev1.Service, err error) {
	return nil, fmt.Errorf("unimplemented GetPodServices(%v)", p)
}

type nsK8sServiceLister struct {
	r  *K8sServiceLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ corev1listers.ServiceNamespaceLister = (*nsK8sServiceLister)(nil)

func (r *nsK8sServiceLister) List(selector labels.Selector) (results []*corev1.Service, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsK8sServiceLister) Get(name string) (*corev1.Service, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// ConfigMapLister is a lister.ConfigMapLister fake for testing.
type ConfigMapLister struct {
	Err   error
	Items []*corev1.ConfigMap
}

// Assert that our fake implements the interface it is faking.
var _ corev1listers.ConfigMapLister = (*ConfigMapLister)(nil)

func (r *ConfigMapLister) List(selector labels.Selector) (results []*corev1.ConfigMap, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
}

func (r *ConfigMapLister) ConfigMaps(namespace string) corev1listers.ConfigMapNamespaceLister {
	return &nsConfigMapLister{r: r, ns: namespace}
}

type nsConfigMapLister struct {
	r  *ConfigMapLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ corev1listers.ConfigMapNamespaceLister = (*nsConfigMapLister)(nil)

func (r *nsConfigMapLister) List(selector labels.Selector) (results []*corev1.ConfigMap, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsConfigMapLister) Get(name string) (*corev1.ConfigMap, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

// VirtualServiceLister is a istiolisters.VirtualServiceLister fake for testing.
type VirtualServiceLister struct {
	Err   error
	Items []*istiov1alpha3.VirtualService
}

// Assert that our fake implements the interface it is faking.
var _ istiolisters.VirtualServiceLister = (*VirtualServiceLister)(nil)

func (r *VirtualServiceLister) List(selector labels.Selector) (results []*istiov1alpha3.VirtualService, err error) {
	for _, elt := range r.Items {
		if selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.Err
}

func (r *VirtualServiceLister) VirtualServices(namespace string) istiolisters.VirtualServiceNamespaceLister {
	return &nsVirtualServiceLister{r: r, ns: namespace}
}

func (r *VirtualServiceLister) GetPodServices(p *corev1.Pod) (results []*istiov1alpha3.VirtualService, err error) {
	return nil, fmt.Errorf("unimplemented GetPodServices(%v)", p)
}

type nsVirtualServiceLister struct {
	r  *VirtualServiceLister
	ns string
}

// Assert that our fake implements the interface it is faking.
var _ istiolisters.VirtualServiceNamespaceLister = (*nsVirtualServiceLister)(nil)

func (r *nsVirtualServiceLister) List(selector labels.Selector) (results []*istiov1alpha3.VirtualService, err error) {
	for _, elt := range r.r.Items {
		if elt.Namespace == r.ns && selector.Matches(labels.Set(elt.Labels)) {
			results = append(results, elt)
		}
	}
	return results, r.r.Err
}

func (r *nsVirtualServiceLister) Get(name string) (*istiov1alpha3.VirtualService, error) {
	for _, s := range r.r.Items {
		if s.Name == name && r.ns == s.Namespace {
			return s, nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}
