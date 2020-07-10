/*
Copyright 2020 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genclient:nonNamespaced
// +genreconciler:krshapedlogic=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Domain is a cluster-scoped resource to configure a proxy pool for a given Route.
type Domain struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the Domain.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec DomainSpec `json:"spec,omitempty"`

	// Status is the current state of the Domain.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status DomainStatus `json:"status,omitempty"`
}

// Verify that Domain adheres to the appropriate interfaces.
var (
	// Check that Doimain may be validated and defaulted.
	_ apis.Validatable = (*Domain)(nil)
	_ apis.Defaultable = (*Domain)(nil)

	// Check that we can create OwnerReferences to a Domain.
	_ kmeta.OwnerRefable = (*Domain)(nil)

	// Check that the type conforms to the duck Knative Resource shape.
	_ duckv1.KRShaped = (*Domain)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DomainList is a collection of Domain objects.
type DomainList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of Domain objects.
	Items []Domain `json:"items"`
}

// DomainSpec describes the Ingress the user wishes to exist.
//
// In general this follows the same shape as K8s Ingress.
// Some notable differences:
// - Backends now can have namespace:
// - Traffic can be split across multiple backends.
// - Timeout & Retry can be configured.
// - Headers can be appended.
// DomainSpec contains the specification of the Domain CRD.
type DomainSpec struct {
	// IngressClass tells what Ingress class annotation to use for Routes associated
	// with this Realm.
	IngressClass string `json:"ingressClass"`

	// Suffix specifies the domain suffix to be used. This field replaces the
	// existing config-domain ConfigMap.  Internal Domains can omit this, in
	// which case we will default to the cluster suffix.
	// +optional
	Suffix string `json:"suffix,omitempty"`

	// LoadBalancers provide addresses (IP addresses, domains) of the load balancers
	// associated with this Domain.  This is used in automatic DNS provisioning like
	// configuration of magic DNS or creating ExternalName services for cluster-local
	// access.
	LoadBalancers []LoadBalancerIngressSpec `json:"loadBalancers"`

	// Configs contains additional pieces of information to configure ingress proxies.
	// +optional
	Configs []IngressConfig `json:"configs,omitempty"`
}

// IngressConfig allows KIngress implementations to add additional information needed
// for configuring the proxies associated with this Domain.
// For examples, in our Istio-based Ingress this will contains all references of
// Istio Gateways associated with this Domain. This could be a reference of a ConfigMap
// owned by the implementation as well.
type IngressConfig struct {
	// Name of the Kingress implementation resource
	// +optional
	Name string `json:"name,omitempty"`
	// Namespace of the Kingress implementation resource
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Type of the Kingress implementation resource
	// +optional
	Type string `json:"type,omitempty"`
}

// LoadBalancerIngressSpec represents the spec of a load-balancer ingress point:
// traffic intended for the service should be sent to an ingress point.
type LoadBalancerIngressSpec struct {
	// IP is set for load-balancer ingress points that are IP based
	// (typically GCE or OpenStack load-balancers)
	// +optional
	IP string `json:"ip,omitempty"`

	// Domain is set for load-balancer ingress points that are DNS based
	// (typically AWS load-balancers)
	// +optional
	Domain string `json:"domain,omitempty"`

	// DomainInternal is set if there is a cluster-local DNS name to access the Ingress.
	//
	// NOTE: This differs from K8s Ingress, since we also desire to have a cluster-local
	//       DNS name to allow routing in case of not having a mesh.
	//
	// +optional
	DomainInternal string `json:"domainInternal,omitempty"`

	// MeshOnly is set if the Ingress is only load-balanced through a Service mesh.
	// +optional
	MeshOnly bool `json:"meshOnly,omitempty"`
}

// DomainStatus will reflect Ready=True if the implementation accepts the Domain data
// as valid.
type DomainStatus struct {
	duckv1.Status `json:",inline"`
}

// GetStatus retrieves the status of the Domain. Implements the KRShaped interface.
func (d *Domain) GetStatus() *duckv1.Status {
	return &d.Status.Status
}
