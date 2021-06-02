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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

// +genclient
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DomainMapping is a mapping from a custom hostname to an Addressable.
type DomainMapping struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the DomainMapping.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec DomainMappingSpec `json:"spec,omitempty"`

	// Status is the current state of the DomainMapping.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status DomainMappingStatus `json:"status,omitempty"`
}

// Verify that DomainMapping adheres to the appropriate interfaces.
var (
	// Check that DomainMapping may be validated and defaulted.
	_ apis.Validatable = (*DomainMapping)(nil)
	_ apis.Defaultable = (*DomainMapping)(nil)

	// Check that the type conforms to the duck Knative Resource shape.
	_ duckv1.KRShaped = (*DomainMapping)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DomainMappingList is a collection of DomainMapping objects.
type DomainMappingList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of DomainMapping objects.
	Items []DomainMapping `json:"items"`
}

// SecretTLS wrapper for TLS SecretName.
type SecretTLS struct {
	// SecretName is the name of the existing secret used to terminate TLS traffic.
	SecretName string `json:"secretName"`
}

// DomainMappingSpec describes the DomainMapping the user wishes to exist.
type DomainMappingSpec struct {
	// Ref specifies the target of the Domain Mapping.
	//
	// The object identified by the Ref must be an Addressable with a URL of the
	// form `{name}.{namespace}.{domain}` where `{domain}` is the cluster domain,
	// and `{name}` and `{namespace}` are the name and namespace of a Kubernetes
	// Service.
	//
	// This contract is satisfied by Knative types such as Knative Services and
	// Knative Routes, and by Kubernetes Services.
	Ref duckv1.KReference `json:"ref"`

	// TLS allows the DomainMapping to terminate TLS traffic with an existing secret.
	// +optional
	TLS *SecretTLS `json:"tls,omitempty"`
}

// DomainMappingStatus describes the current state of the DomainMapping.
type DomainMappingStatus struct {
	duckv1.Status `json:",inline"`

	// URL is the URL of this DomainMapping.
	// +optional
	URL *apis.URL `json:"url,omitempty"`

	// Address holds the information needed for a DomainMapping to be the target of an event.
	// +optional
	Address *duckv1.Addressable `json:"address,omitempty"`
}

const (
	// DomainMappingConditionReady is set when the DomainMapping is configured
	// and the Ingress is ready.
	DomainMappingConditionReady = apis.ConditionReady

	// DomainMappingConditionReferenceResolved reflects whether the Ref
	// has been successfully resolved to an existing object.
	DomainMappingConditionReferenceResolved apis.ConditionType = "ReferenceResolved"

	// DomainMappingConditionIngressReady reflects the readiness of the
	// underlying Ingress resource.
	DomainMappingConditionIngressReady apis.ConditionType = "IngressReady"

	// DomainMappingConditionDomainClaimed reflects that the ClusterDomainClaim
	// for this DomainMapping exists, and is owned by this DomainMapping.
	DomainMappingConditionDomainClaimed apis.ConditionType = "DomainClaimed"

	// DomainMappingConditionCertificateProvisioned is set to False when the
	// Knative Certificates fail to be provisioned for the DomainMapping.
	DomainMappingConditionCertificateProvisioned apis.ConditionType = "CertificateProvisioned"
)

// GetStatus retrieves the status of the DomainMapping. Implements the KRShaped interface.
func (dm *DomainMapping) GetStatus() *duckv1.Status {
	return &dm.Status.Status
}
