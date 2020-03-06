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
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Foo is for testing.
type Foo struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Foo (from the client).
	// +optional
	Spec FooSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Foo (from the controller).
	// +optional
	Status FooStatus `json:"status,omitempty"`
}

// Check that Foo can be validated and defaulted.
var _ apis.Validatable = (*Foo)(nil)
var _ apis.Defaultable = (*Foo)(nil)
var _ kmeta.OwnerRefable = (*Foo)(nil)

// FooSpec holds the desired state of the Foo (from the client).
type FooSpec struct{}

// FooStatus communicates the observed state of the Foo (from the controller).
type FooStatus struct {
	duckv1.Status `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FooList is a list of Foo resources
type FooList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Foo `json:"items"`
}

// -- lifecycle --

func (fs *FooStatus) InitializeConditions() {}

// GetGroupVersionKind implements kmeta.OwnerRefable
func (f *Foo) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Bar")
}

// -- Defaults --

// SetDefaults implements apis.Defaultable
func (f *Foo) SetDefaults(ctx context.Context) {
	// Nothing to default.
}

// -- Validation --

// Validate implements apis.Validatable
func (f *Foo) Validate(ctx context.Context) *apis.FieldError {
	// Nothing to validate.
	return nil
}
