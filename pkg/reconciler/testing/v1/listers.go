/*
Copyright 2020 The Knative Authors

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

package v1

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	autoscalingv2beta1listers "k8s.io/client-go/listers/autoscaling/v2beta1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	cachingv1alpha1 "knative.dev/caching/pkg/apis/caching/v1alpha1"
	fakecachingclientset "knative.dev/caching/pkg/client/clientset/versioned/fake"
	cachinglisters "knative.dev/caching/pkg/client/listers/caching/v1alpha1"
	networking "knative.dev/networking/pkg/apis/networking/v1alpha1"
	fakenetworkingclientset "knative.dev/networking/pkg/client/clientset/versioned/fake"
	networkinglisters "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/reconciler/testing"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakeservingclientset "knative.dev/serving/pkg/client/clientset/versioned/fake"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"
	servingv1alpha1listers "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
)

var clientSetSchemes = []func(*runtime.Scheme) error{
	autoscalingv2beta1.AddToScheme,
	fakecachingclientset.AddToScheme,
	fakekubeclientset.AddToScheme,
	fakenetworkingclientset.AddToScheme,
	fakeservingclientset.AddToScheme,
}

// Listers provides access to Listers for various objects.
type Listers struct {
	sorter testing.ObjectSorter
}

// NewListers returns a new Listers.
func NewListers(objs []runtime.Object) Listers {
	scheme := NewScheme()

	ls := Listers{
		sorter: testing.NewObjectSorter(scheme),
	}

	ls.sorter.AddObjects(objs...)

	return ls
}

// NewScheme returns a new runtime.Scheme.
func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, addTo := range clientSetSchemes {
		addTo(scheme)
	}
	return scheme
}

// NewScheme returns a new runtime.Scheme.
func (*Listers) NewScheme() *runtime.Scheme {
	return NewScheme()
}

// IndexerFor returns the indexer for the given object.
func (l *Listers) IndexerFor(obj runtime.Object) cache.Indexer {
	return l.sorter.IndexerForObjectType(obj)
}

// GetKubeObjects returns the runtime.Objects from the fakekubeclientset.
func (l *Listers) GetKubeObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakekubeclientset.AddToScheme)
}

// GetCachingObjects returns the runtime.Objects from the fakecachingclientset.
func (l *Listers) GetCachingObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakecachingclientset.AddToScheme)
}

// GetServingObjects returns the runtime.Objects from the fakeservingclientset.
func (l *Listers) GetServingObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakeservingclientset.AddToScheme)
}

// GetNetworkingObjects returns the runtime.Objects from the fakenetworkingclientset.
func (l *Listers) GetNetworkingObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakenetworkingclientset.AddToScheme)
}

// GetServiceLister returns a lister for Service objects.
func (l *Listers) GetServiceLister() servinglisters.ServiceLister {
	return servinglisters.NewServiceLister(l.IndexerFor(&v1.Service{}))
}

// GetRouteLister returns a lister for Route objects.
func (l *Listers) GetRouteLister() servinglisters.RouteLister {
	return servinglisters.NewRouteLister(l.IndexerFor(&v1.Route{}))
}

// GetDomainMappingLister returns a lister for DomainMapping objects.
func (l *Listers) GetDomainMappingLister() servingv1alpha1listers.DomainMappingLister {
	return servingv1alpha1listers.NewDomainMappingLister(l.IndexerFor(&v1alpha1.DomainMapping{}))
}

// GetServerlessServiceLister returns a lister for the ServerlessService objects.
func (l *Listers) GetServerlessServiceLister() networkinglisters.ServerlessServiceLister {
	return networkinglisters.NewServerlessServiceLister(l.IndexerFor(&networking.ServerlessService{}))
}

// GetConfigurationLister gets the Configuration lister.
func (l *Listers) GetConfigurationLister() servinglisters.ConfigurationLister {
	return servinglisters.NewConfigurationLister(l.IndexerFor(&v1.Configuration{}))
}

// GetRevisionLister gets the Revision lister.
func (l *Listers) GetRevisionLister() servinglisters.RevisionLister {
	return servinglisters.NewRevisionLister(l.IndexerFor(&v1.Revision{}))
}

// GetPodAutoscalerLister gets the PodAutoscaler lister.
func (l *Listers) GetPodAutoscalerLister() palisters.PodAutoscalerLister {
	return palisters.NewPodAutoscalerLister(l.IndexerFor(&autoscalingv1alpha1.PodAutoscaler{}))
}

// GetMetricLister returns a lister for the Metric objects.
func (l *Listers) GetMetricLister() palisters.MetricLister {
	return palisters.NewMetricLister(l.IndexerFor(&autoscalingv1alpha1.Metric{}))
}

// GetHorizontalPodAutoscalerLister gets lister for HorizontalPodAutoscaler resources.
func (l *Listers) GetHorizontalPodAutoscalerLister() autoscalingv2beta1listers.HorizontalPodAutoscalerLister {
	return autoscalingv2beta1listers.NewHorizontalPodAutoscalerLister(l.IndexerFor(&autoscalingv2beta1.HorizontalPodAutoscaler{}))
}

// GetIngressLister get lister for Ingress resource.
func (l *Listers) GetIngressLister() networkinglisters.IngressLister {
	return networkinglisters.NewIngressLister(l.IndexerFor(&networking.Ingress{}))
}

// GetCertificateLister get lister for Certificate resource.
func (l *Listers) GetCertificateLister() networkinglisters.CertificateLister {
	return networkinglisters.NewCertificateLister(l.IndexerFor(&networking.Certificate{}))
}

// GetKnCertificateLister gets lister for Knative Certificate resource.
func (l *Listers) GetKnCertificateLister() networkinglisters.CertificateLister {
	return networkinglisters.NewCertificateLister(l.IndexerFor(&networking.Certificate{}))
}

// GetImageLister returns a lister for Image objects.
func (l *Listers) GetImageLister() cachinglisters.ImageLister {
	return cachinglisters.NewImageLister(l.IndexerFor(&cachingv1alpha1.Image{}))
}

// GetDeploymentLister returns a lister for Deployment objects.
func (l *Listers) GetDeploymentLister() appsv1listers.DeploymentLister {
	return appsv1listers.NewDeploymentLister(l.IndexerFor(&appsv1.Deployment{}))
}

// GetK8sServiceLister returns a lister for K8sService objects.
func (l *Listers) GetK8sServiceLister() corev1listers.ServiceLister {
	return corev1listers.NewServiceLister(l.IndexerFor(&corev1.Service{}))
}

// GetEndpointsLister returns a lister for Endpoints objects.
func (l *Listers) GetEndpointsLister() corev1listers.EndpointsLister {
	return corev1listers.NewEndpointsLister(l.IndexerFor(&corev1.Endpoints{}))
}

// GetPodsLister gets lister for pods.
func (l *Listers) GetPodsLister() corev1listers.PodLister {
	return corev1listers.NewPodLister(l.IndexerFor(&corev1.Pod{}))
}

// GetNamespaceLister gets lister for Namespace resource.
func (l *Listers) GetNamespaceLister() corev1listers.NamespaceLister {
	return corev1listers.NewNamespaceLister(l.IndexerFor(&corev1.Namespace{}))
}
