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
)

// GetIngress returns an ClsuterIngress object of type IngressAccessor
func (r *Reconciler) GetIngress(ns, name string) (v1alpha1.IngressAccessor, error) {
	return r.clusterIngressLister.Get(name)
}

// PatchIngress invokes APIs tp Patch an ClusterIngress
func (r *Reconciler) PatchIngress(ns, name string, pt types.PatchType, data []byte, subresources ...string) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Patch(name, pt, data, subresources...)
}

// UpdateIngress invokes APIs tp Update an ClusterIngress
func (r *Reconciler) UpdateIngress(ia v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Update(ia.(*v1alpha1.ClusterIngress))
}

// UpdateIngressStatus invokes APIs tp Update an IngressStatus
func (r *Reconciler) UpdateIngressStatus(ia v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	return r.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().UpdateStatus(ia.(*v1alpha1.ClusterIngress))
}
