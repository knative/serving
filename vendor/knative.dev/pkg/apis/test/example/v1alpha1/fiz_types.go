/*
Copyright 2020 The Knative Authors.

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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genclient:nonNamespaced
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterFiz is for testing.
type ClusterFiz struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the ClusterFiz (from the client).
	// +optional
	Spec ClusterFizSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the ClusterFiz (from the controller).
	// +optional
	Status ClusterFizStatus `json:"status,omitempty"`
}

// Check that ClusterFiz can be validated and defaulted.
var _ apis.Validatable = (*ClusterFiz)(nil)
var _ apis.Defaultable = (*ClusterFiz)(nil)
var _ kmeta.OwnerRefable = (*ClusterFiz)(nil)

// ClusterFizSpec holds the desired state of the ClusterFiz (from the client).
type ClusterFizSpec struct{}

// ClusterFizStatus communicates the observed state of the ClusterFiz (from the controller).
type ClusterFizStatus struct {
	duckv1.Status `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterFizList is a list of ClusterFiz resources
type ClusterFizList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ClusterFiz `json:"items"`
}

// -- lifecycle --

func (fs *ClusterFizStatus) InitializeConditions() {}

// GetGroupVersionKind implements kmeta.OwnerRefable
func (f *ClusterFiz) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Bar")
}

// -- Defaults --

// SetDefaults implements apis.Defaultable
func (f *ClusterFiz) SetDefaults(ctx context.Context) {
	// Nothing to default.
}

// -- Validation --

// Validate implements apis.Validatable
func (f *ClusterFiz) Validate(ctx context.Context) *apis.FieldError {
	// Nothing to validate.
	return nil
}
