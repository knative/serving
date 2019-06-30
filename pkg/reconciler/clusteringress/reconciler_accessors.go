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

package clusteringress

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

// clusterIngressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - clusteringresses.networking.internal.knative.dev
var (
	clusterIngressResource  = v1alpha1.Resource("clusteringresses")
	clusterIngressFinalizer = clusterIngressResource.String()
)

func (r *Reconciler) DeepCopy(ia v1alpha1.IngressAccessor) v1alpha1.IngressAccessor {
	return ia.(*v1alpha1.ClusterIngress).DeepCopy()
}

func (r *Reconciler) GetFinalizer() string {
	return clusterIngressFinalizer
}

func (r *Reconciler) GetGatewayLister() istiolisters.GatewayLister {
	return r.GatewayLister
}

func (r *Reconciler) GetIngress(ns, name string) (v1alpha1.IngressAccessor, error) {
	return r.clusterIngressLister.Get(name)
}

func (r *Reconciler) GetKubeClientSet() kubernetes.Interface {
	return r.KubeClientSet
}

func (r *Reconciler) GetRecorder() record.EventRecorder {
	return r.Recorder
}

func (r *Reconciler) GetSecretLister() corev1listers.SecretLister {
	return r.SecretLister
}

func (r *Reconciler) GetSharedClientSet() sharedclientset.Interface {
	return r.SharedClientSet
}

func (r *Reconciler) GetTracker() tracker.Interface {
	return r.Tracker
}

func (r *Reconciler) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return r.VirtualServiceLister
}

func (r *Reconciler) PatchIngress(ns, name string, pt types.PatchType, data []byte, subresources ...string) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Patch(name, pt, data, subresources...)
}

func (r *Reconciler) UpdateIngress(ia v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Update(ia.(*v1alpha1.ClusterIngress))
}
func (r *Reconciler) UpdateIngressStatus(ia v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().UpdateStatus(ia.(*v1alpha1.ClusterIngress))
}
