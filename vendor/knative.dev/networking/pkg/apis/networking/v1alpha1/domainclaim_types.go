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
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterDomainClaim is a cluster-wide reservation for a particular domain name.
type ClusterDomainClaim struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the ClusterDomainClaim.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec ClusterDomainClaimSpec `json:"spec,omitempty"`
}

var (
	// Check that we can create OwnerReferences to a ClusterDomainClaim.
	_ kmeta.OwnerRefable = (*ClusterDomainClaim)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterDomainClaimList is a collection of ClusterDomainClaim objects.
type ClusterDomainClaimList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ClusterDomainClaim objects.
	Items []ClusterDomainClaim `json:"items"`
}

// ClusterDomainClaimSpec is the desired state of the ClusterDomainClaim.
// Its only field is `namespace`, which controls which namespace currently owns
// the ability to create a DomainMapping with the ClusterDomainClaim's name.
type ClusterDomainClaimSpec struct {
	// Namespace is the namespace which is allowed to create a DomainMapping
	// using this ClusterDomainClaim's name.
	Namespace string `json:"namespace"`
}
