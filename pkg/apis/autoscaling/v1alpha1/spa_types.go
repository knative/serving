/*
Copyright 2018 The Knative Authors

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
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genreconciler:class=autoscaling.knative.dev/class
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodAutoscaler is a Knative abstraction that encapsulates the interface by which Knative
// components instantiate autoscalers.  This definition is an abstraction that may be backed
// by multiple definitions.  For more information, see the Knative Pluggability presentation:
// https://docs.google.com/presentation/d/19vW9HFZ6Puxt31biNZF3uLRejDmu82rxJIk1cWmxF7w/edit
type StagePodAutoscaler struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the PodAutoscaler (from the client).
	// +optional
	Spec StagePodAutoscalerSpec `json:"spec,omitempty"`
}

// Verify that PodAutoscaler adheres to the appropriate interfaces.
var (
	// Check that PodAutoscaler can be validated and can be defaulted.
	_ apis.Validatable = (*StagePodAutoscaler)(nil)
	_ apis.Defaultable = (*StagePodAutoscaler)(nil)

	// Check that we can create OwnerReferences to a PodAutoscaler.
	_ kmeta.OwnerRefable = (*StagePodAutoscaler)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodAutoscalerList is a list of PodAutoscaler resources
type StagePodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []StagePodAutoscaler `json:"items"`
}

type StagePodAutoscalerSpec struct {
	// MinScale sets the lower bound for the number of the replicas.
	// +optional
	MinScale *int32 `json:"minScale,omitempty"`

	// MaxScale sets the upper bound for the number of the replicas.
	// +optional
	MaxScale *int32 `json:"maxScale,omitempty"`
}
