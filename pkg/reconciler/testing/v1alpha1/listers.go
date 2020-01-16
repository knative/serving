/*
Copyright 2019 The Knative Authors

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
	acmev1alpha2 "github.com/jetstack/cert-manager/pkg/apis/acme/v1alpha2"
	cmv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	istiov1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
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
	"knative.dev/pkg/reconciler/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	networking "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	acmelisters "knative.dev/serving/pkg/client/certmanager/listers/acme/v1alpha2"
	certmanagerlisters "knative.dev/serving/pkg/client/certmanager/listers/certmanager/v1alpha2"
	fakeservingclientset "knative.dev/serving/pkg/client/clientset/versioned/fake"
	fakeistioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned/fake"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	networkinglisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
)

var clientSetSchemes = []func(*runtime.Scheme) error{
	fakekubeclientset.AddToScheme,
	fakeistioclientset.AddToScheme,
	fakeservingclientset.AddToScheme,
	fakecachingclientset.AddToScheme,
	cmv1alpha2.AddToScheme,
	acmev1alpha2.AddToScheme,
	autoscalingv2beta1.AddToScheme,
}

type Listers struct {
	sorter testing.ObjectSorter
}

func NewListers(objs []runtime.Object) Listers {
	scheme := NewScheme()

	ls := Listers{
		sorter: testing.NewObjectSorter(scheme),
	}

	ls.sorter.AddObjects(objs...)

	return ls
}

func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, addTo := range clientSetSchemes {
		addTo(scheme)
	}
	return scheme
}

func (*Listers) NewScheme() *runtime.Scheme {
	return NewScheme()
}

// IndexerFor returns the indexer for the given object.
func (l *Listers) IndexerFor(obj runtime.Object) cache.Indexer {
	return l.sorter.IndexerForObjectType(obj)
}

func (l *Listers) GetKubeObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakekubeclientset.AddToScheme)
}

func (l *Listers) GetCachingObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakecachingclientset.AddToScheme)
}

func (l *Listers) GetServingObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakeservingclientset.AddToScheme)
}

func (l *Listers) GetIstioObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakeistioclientset.AddToScheme)
}

// GetCMCertificateObjects gets a list of Cert-Manager Certificate objects.
func (l *Listers) GetCMCertificateObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(cmv1alpha2.AddToScheme)
}

func (l *Listers) GetServiceLister() servinglisters.ServiceLister {
	return servinglisters.NewServiceLister(l.IndexerFor(&v1alpha1.Service{}))
}

func (l *Listers) GetRouteLister() servinglisters.RouteLister {
	return servinglisters.NewRouteLister(l.IndexerFor(&v1alpha1.Route{}))
}

// GetServerlessServiceLister returns a lister for the ServerlessService objects.
func (l *Listers) GetServerlessServiceLister() networkinglisters.ServerlessServiceLister {
	return networkinglisters.NewServerlessServiceLister(l.IndexerFor(&networking.ServerlessService{}))
}

func (l *Listers) GetConfigurationLister() servinglisters.ConfigurationLister {
	return servinglisters.NewConfigurationLister(l.IndexerFor(&v1alpha1.Configuration{}))
}

func (l *Listers) GetRevisionLister() servinglisters.RevisionLister {
	return servinglisters.NewRevisionLister(l.IndexerFor(&v1alpha1.Revision{}))
}

func (l *Listers) GetPodAutoscalerLister() palisters.PodAutoscalerLister {
	return palisters.NewPodAutoscalerLister(l.IndexerFor(&av1alpha1.PodAutoscaler{}))
}

// GetMetricLister returns a lister for the Metric objects.
func (l *Listers) GetMetricLister() palisters.MetricLister {
	return palisters.NewMetricLister(l.IndexerFor(&av1alpha1.Metric{}))
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

func (l *Listers) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return istiolisters.NewVirtualServiceLister(l.IndexerFor(&istiov1alpha3.VirtualService{}))
}

// GetGatewayLister gets lister for Istio Gateway resource.
func (l *Listers) GetGatewayLister() istiolisters.GatewayLister {
	return istiolisters.NewGatewayLister(l.IndexerFor(&istiov1alpha3.Gateway{}))
}

// GetKnCertificateLister gets lister for Knative Certificate resource.
func (l *Listers) GetKnCertificateLister() networkinglisters.CertificateLister {
	return networkinglisters.NewCertificateLister(l.IndexerFor(&networking.Certificate{}))
}

// GetCMCertificateLister gets lister for Cert Manager Certificate resource.
func (l *Listers) GetCMCertificateLister() certmanagerlisters.CertificateLister {
	return certmanagerlisters.NewCertificateLister(l.IndexerFor(&cmv1alpha2.Certificate{}))
}

// GetCMClusterIssuerLister gets lister for Cert Manager ClusterIssuer resource.
func (l *Listers) GetCMClusterIssuerLister() certmanagerlisters.ClusterIssuerLister {
	return certmanagerlisters.NewClusterIssuerLister(l.IndexerFor(&cmv1alpha2.ClusterIssuer{}))
}

// GetCMChallengeLister gets lister for Cert Manager Challenge resource.
func (l *Listers) GetCMChallengeLister() acmelisters.ChallengeLister {
	return acmelisters.NewChallengeLister(l.IndexerFor(&acmev1alpha2.Challenge{}))
}

func (l *Listers) GetImageLister() cachinglisters.ImageLister {
	return cachinglisters.NewImageLister(l.IndexerFor(&cachingv1alpha1.Image{}))
}

func (l *Listers) GetDeploymentLister() appsv1listers.DeploymentLister {
	return appsv1listers.NewDeploymentLister(l.IndexerFor(&appsv1.Deployment{}))
}

func (l *Listers) GetK8sServiceLister() corev1listers.ServiceLister {
	return corev1listers.NewServiceLister(l.IndexerFor(&corev1.Service{}))
}

func (l *Listers) GetEndpointsLister() corev1listers.EndpointsLister {
	return corev1listers.NewEndpointsLister(l.IndexerFor(&corev1.Endpoints{}))
}

func (l *Listers) GetSecretLister() corev1listers.SecretLister {
	return corev1listers.NewSecretLister(l.IndexerFor(&corev1.Secret{}))
}

func (l *Listers) GetConfigMapLister() corev1listers.ConfigMapLister {
	return corev1listers.NewConfigMapLister(l.IndexerFor(&corev1.ConfigMap{}))
}

// GetNamespaceLister gets lister for Namespace resource.
func (l *Listers) GetNamespaceLister() corev1listers.NamespaceLister {
	return corev1listers.NewNamespaceLister(l.IndexerFor(&corev1.Namespace{}))
}
