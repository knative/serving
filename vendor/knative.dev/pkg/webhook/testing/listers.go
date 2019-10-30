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

package testing

import (
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	admissionlisters "k8s.io/client-go/listers/admissionregistration/v1beta1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	autoscalingv2beta1listers "k8s.io/client-go/listers/autoscaling/v2beta1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	istiov1alpha3 "knative.dev/pkg/apis/istio/v1alpha3"
	fakesharedclientset "knative.dev/pkg/client/clientset/versioned/fake"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/reconciler/testing"
)

var clientSetSchemes = []func(*runtime.Scheme) error{
	fakekubeclientset.AddToScheme,
	fakesharedclientset.AddToScheme,
	autoscalingv2beta1.AddToScheme,
}

// Listers is used to synthesize informer-style Listers from fixed lists of resources in tests.
type Listers struct {
	sorter testing.ObjectSorter
}

// NewListers constructs a Listers from a collection of objects.
func NewListers(objs []runtime.Object) Listers {
	scheme := NewScheme()

	ls := Listers{
		sorter: testing.NewObjectSorter(scheme),
	}

	ls.sorter.AddObjects(objs...)

	return ls
}

// NewScheme constructs a scheme from the set of client schemes supported by this package.
func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, addTo := range clientSetSchemes {
		addTo(scheme)
	}
	return scheme
}

// NewScheme constructs a scheme from the set of client schemes supported by this lister.
func (*Listers) NewScheme() *runtime.Scheme {
	return NewScheme()
}

// IndexerFor returns the indexer for the given object.
func (l *Listers) IndexerFor(obj runtime.Object) cache.Indexer {
	return l.sorter.IndexerForObjectType(obj)
}

// GetKubeObjects filters the Listers initial list of objects to built-in types
func (l *Listers) GetKubeObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakekubeclientset.AddToScheme)
}

// GetSharedObjects filters the Listers initial list of objects to types defined in knative/pkg
func (l *Listers) GetSharedObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakesharedclientset.AddToScheme)
}

// GetHorizontalPodAutoscalerLister gets lister for HorizontalPodAutoscaler resources.
func (l *Listers) GetHorizontalPodAutoscalerLister() autoscalingv2beta1listers.HorizontalPodAutoscalerLister {
	return autoscalingv2beta1listers.NewHorizontalPodAutoscalerLister(l.IndexerFor(&autoscalingv2beta1.HorizontalPodAutoscaler{}))
}

// GetVirtualServiceLister gets lister for Istio VirtualService resource.
func (l *Listers) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return istiolisters.NewVirtualServiceLister(l.IndexerFor(&istiov1alpha3.VirtualService{}))
}

// GetGatewayLister gets lister for Istio Gateway resource.
func (l *Listers) GetGatewayLister() istiolisters.GatewayLister {
	return istiolisters.NewGatewayLister(l.IndexerFor(&istiov1alpha3.Gateway{}))
}

// GetDeploymentLister gets lister for K8s Deployment resource.
func (l *Listers) GetDeploymentLister() appsv1listers.DeploymentLister {
	return appsv1listers.NewDeploymentLister(l.IndexerFor(&appsv1.Deployment{}))
}

// GetK8sServiceLister gets lister for K8s Service resource.
func (l *Listers) GetK8sServiceLister() corev1listers.ServiceLister {
	return corev1listers.NewServiceLister(l.IndexerFor(&corev1.Service{}))
}

// GetEndpointsLister gets lister for K8s Endpoints resource.
func (l *Listers) GetEndpointsLister() corev1listers.EndpointsLister {
	return corev1listers.NewEndpointsLister(l.IndexerFor(&corev1.Endpoints{}))
}

// GetSecretLister gets lister for K8s Secret resource.
func (l *Listers) GetSecretLister() corev1listers.SecretLister {
	return corev1listers.NewSecretLister(l.IndexerFor(&corev1.Secret{}))
}

// GetConfigMapLister gets lister for K8s ConfigMap resource.
func (l *Listers) GetConfigMapLister() corev1listers.ConfigMapLister {
	return corev1listers.NewConfigMapLister(l.IndexerFor(&corev1.ConfigMap{}))
}

// GetNamespaceLister gets lister for Namespace resource.
func (l *Listers) GetNamespaceLister() corev1listers.NamespaceLister {
	return corev1listers.NewNamespaceLister(l.IndexerFor(&corev1.Namespace{}))
}

// GetMutatingWebhookConfigurationLister gets lister for K8s MutatingWebhookConfiguration resource.
func (l *Listers) GetMutatingWebhookConfigurationLister() admissionlisters.MutatingWebhookConfigurationLister {
	return admissionlisters.NewMutatingWebhookConfigurationLister(l.IndexerFor(&admissionregistrationv1beta1.MutatingWebhookConfiguration{}))
}

// GetValidatingWebhookConfigurationLister gets lister for K8s ValidatingWebhookConfiguration resource.
func (l *Listers) GetValidatingWebhookConfigurationLister() admissionlisters.ValidatingWebhookConfigurationLister {
	return admissionlisters.NewValidatingWebhookConfigurationLister(l.IndexerFor(&admissionregistrationv1beta1.ValidatingWebhookConfiguration{}))
}
