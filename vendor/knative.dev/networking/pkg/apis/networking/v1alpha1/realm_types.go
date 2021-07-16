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

// Realm is a cluster-scoped resource that specifies a Route visibility.
type Realm struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the Realm.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec RealmSpec `json:"spec,omitempty"`

	// Status is the current state of the Realm.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status RealmStatus `json:"status,omitempty"`
}

// Verify that Realm adheres to the appropriate interfaces.
var (
	// Check that Realm may be validated and defaulted.
	_ apis.Validatable = (*Realm)(nil)
	_ apis.Defaultable = (*Realm)(nil)

	// Check that we can create OwnerReferences to a Realm.
	_ kmeta.OwnerRefable = (*Realm)(nil)

	// Check that the type conforms to the duck Knative Resource shape.
	_ duckv1.KRShaped = (*Realm)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RealmList is a collection of Realm objects.
type RealmList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata.
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of Realm objects.
	Items []Realm `json:"items"`
}

// RealmSpec defines the specifications of a Realm.
// Realms specify an internal and external Domain, but disallow arbitrary combinations.
// Operators can create combinations of Domains that makes sense for their clusters,
// and developers can only switch between these predefined Realms.
//
// Conditions: Currently each Realm can have only two Domains.
// That fits well with the way we assign Route.Status.URL and Route.Status.Domain.
// All Domains in the same Realm must share the same ingress class annotation.
type RealmSpec struct {
	// External contains the name of the Domain resource corresponding with traffic
	// originating from outside of the cluster.  Could be omitted for a Realm without
	// external access.
	// +optional
	External string `json:"external,omitempty"`

	// Internal contains the name of the Domain resource corresponding with traffic
	// originating from inside of the cluster.  Could be omitted for a Realm without
	// internal access.
	// +optional
	Internal string `json:"internal,omitempty"`
}

// RealmStatus will reflect Ready=True if the implementation accepts the Realm data
// as valid.
type RealmStatus struct {
	duckv1.Status `json:",inline"`
}

// GetStatus retrieves the status of the Realm. Implements the KRShaped interface.
func (r *Realm) GetStatus() *duckv1.Status {
	return &r.Status.Status
}
