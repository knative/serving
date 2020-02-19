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
// +genreconciler:class=example.com/filter.class
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Bar is for testing.
type Bar struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Bar (from the client).
	// +optional
	Spec BarSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Bar (from the controller).
	// +optional
	Status BarStatus `json:"status,omitempty"`
}

// Check that Bar can be validated and defaulted.
var _ apis.Validatable = (*Bar)(nil)
var _ apis.Defaultable = (*Bar)(nil)
var _ kmeta.OwnerRefable = (*Bar)(nil)

// BarSpec holds the desired state of the Bar (from the client).
type BarSpec struct{}

// BarStatus communicates the observed state of the Bar (from the controller).
type BarStatus struct {
	duckv1.Status `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BarList is a list of Bar resources
type BarList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Bar `json:"items"`
}

// -- lifecycle --

func (bs *BarStatus) InitializeConditions() {}

// GetGroupVersionKind implements kmeta.OwnerRefable
func (b *Bar) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Bar")
}

// -- Defaults --

// SetDefaults implements apis.Defaultable
func (b *Bar) SetDefaults(ctx context.Context) {
	// Nothing to default.
}

// -- Validation --

// Validate implements apis.Validatable
func (b *Bar) Validate(ctx context.Context) *apis.FieldError {
	// Nothing to validate.
	return nil
}
