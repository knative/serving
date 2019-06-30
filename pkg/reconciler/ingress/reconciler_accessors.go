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

package ingress

import (
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	sharedclientset "knative.dev/pkg/client/clientset/versioned"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/tracker"
)

// ingressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - ingresses.networking.internal.knative.dev
var (
	ingressResource  = v1alpha1.Resource("ingresses")
	ingressFinalizer = ingressResource.String()
)

// ReconcilerAccessor defines functions that access reconciler data specific to Ingress types
type ReconcilerAccessor interface {
	DeepCopy(v1alpha1.IngressAccessor) v1alpha1.IngressAccessor

	GetFinalizer() string
	GetGatewayLister() istiolisters.GatewayLister
	GetIngress(ns, name string) (v1alpha1.IngressAccessor, error)
	GetKubeClientSet() kubernetes.Interface
	GetRecorder() record.EventRecorder
	GetSecretLister() corev1listers.SecretLister
	GetSharedClientSet() sharedclientset.Interface
	GetTracker() tracker.Interface
	GetVirtualServiceLister() istiolisters.VirtualServiceLister

	PatchIngress(ns, name string, pt types.PatchType, data []byte, subresources ...string) (v1alpha1.IngressAccessor, error)

	UpdateIngress(v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error)
	UpdateIngressStatus(v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error)
}

// DeepCopy returns a deep copied Ingress object of type IngressAccessor
func (r *Reconciler) DeepCopy(ia v1alpha1.IngressAccessor) v1alpha1.IngressAccessor {
	return ia.(*v1alpha1.Ingress).DeepCopy()
}

// GetFinalizer returns Ingress Finalizer
func (r *Reconciler) GetFinalizer() string {
	return ingressFinalizer
}

// GetGatewayLister returns GatewayLister belongs to this Reconciler
func (r *Reconciler) GetGatewayLister() istiolisters.GatewayLister {
	return r.GatewayLister
}

// GetIngress returns an Ingress object of type IngressAccessor
func (r *Reconciler) GetIngress(ns, name string) (v1alpha1.IngressAccessor, error) {
	return r.ingressLister.Ingresses(ns).Get(name)
}

// GetKubeClientSet returns KubeClientSet belongs to this Reconciler
func (r *Reconciler) GetKubeClientSet() kubernetes.Interface {
	return r.KubeClientSet
}

// GetRecorder returns Recorder belongs to this Reconciler
func (r *Reconciler) GetRecorder() record.EventRecorder {
	return r.Recorder
}

// GetSecretLister returns SecretLister belongs to this Reconciler
func (r *Reconciler) GetSecretLister() corev1listers.SecretLister {
	return r.SecretLister
}

// GetSharedClientSet returns SharedClientSet belongs to this Reconciler
func (r *Reconciler) GetSharedClientSet() sharedclientset.Interface {
	return r.SharedClientSet
}

// GetTracker returns Tracker belongs to this Reconciler
func (r *Reconciler) GetTracker() tracker.Interface {
	return r.Tracker
}

// GetVirtualServiceLister returns VirtualServiceLister belongs to this Reconciler
func (r *Reconciler) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return r.VirtualServiceLister
}

// PatchIngress invokes APIs tp Patch an Ingress
func (r *Reconciler) PatchIngress(ns, name string, pt types.PatchType, data []byte, subresources ...string) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().Ingresses(ns).Patch(name, pt, data, subresources...)
}

// UpdateIngress invokes APIs tp Update an Ingress
func (r *Reconciler) UpdateIngress(ia v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().Ingresses(ia.GetObjectMeta().GetNamespace()).Update(ia.(*v1alpha1.Ingress))
}

// UpdateIngressStatus invokes APIs tp Update an IngressStatus
func (r *Reconciler) UpdateIngressStatus(ia v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().Ingresses(ia.GetObjectMeta().GetNamespace()).UpdateStatus(ia.(*v1alpha1.Ingress))
}
