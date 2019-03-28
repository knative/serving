/*
Copyright 2018 The Knative Authors.

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
	"github.com/knative/pkg/apis"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/kmeta"
	net "github.com/knative/serving/pkg/apis/networking"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodAutoscaler is a Knative abstraction that encapsulates the interface by which Knative
// components instantiate autoscalers.  This definition is an abstraction that may be backed
// by multiple definitions.  For more information, see the Knative Pluggability presentation:
// https://docs.google.com/presentation/d/10KWynvAJYuOEWy69VBa6bHJVCqIsz1TNdEKosNvcpPY/edit
type PodAutoscaler struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the PodAutoscaler (from the client).
	// +optional
	Spec PodAutoscalerSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the PodAutoscaler (from the controller).
	// +optional
	Status PodAutoscalerStatus `json:"status,omitempty"`
}

// Verify that PodAutoscaler adheres to the appropriate interfaces.
var (
	// Check that PodAutoscaler can be validated, can be defaulted, and has immutable fields.
	_ apis.Validatable = (*PodAutoscaler)(nil)
	_ apis.Defaultable = (*PodAutoscaler)(nil)
	_ apis.Immutable   = (*PodAutoscaler)(nil)

	// Check that we can create OwnerReferences to a PodAutoscaler.
	_ kmeta.OwnerRefable = (*PodAutoscaler)(nil)
)

// PodAutoscalerSpec holds the desired state of the PodAutoscaler (from the client).
type PodAutoscalerSpec struct {
	// DeprecatedGeneration was used prior in Kubernetes versions <1.11
	// when metadata.generation was not being incremented by the api server
	//
	// This property will be dropped in future Knative releases and should
	// not be used - use metadata.generation
	//
	// Tracking issue: https://github.com/knative/serving/issues/643
	//
	// +optional
	DeprecatedGeneration int64 `json:"generation,omitempty"`

	// ConcurrencyModel specifies the desired concurrency model
	// (Single or Multi) for the scale target. Defaults to Multi.
	// Deprecated in favor of ContainerConcurrency.
	// +optional
	ConcurrencyModel servingv1alpha1.RevisionRequestConcurrencyModelType `json:"concurrencyModel,omitempty"`

	// ContainerConcurrency specifies the maximum allowed
	// in-flight (concurrent) requests per container of the Revision.
	// Defaults to `0` which means unlimited concurrency.
	// This field replaces ConcurrencyModel. A value of `1`
	// is equivalent to `Single` and `0` is equivalent to `Multi`.
	// +optional
	ContainerConcurrency servingv1alpha1.RevisionContainerConcurrencyType `json:"containerConcurrency,omitempty"`

	// ScaleTargetRef defines the /scale-able resource that this PodAutoscaler
	// is responsible for quickly right-sizing.
	ScaleTargetRef autoscalingv1.CrossVersionObjectReference `json:"scaleTargetRef"`

	// ServiceName holds the name of a core Kubernetes Service resource that
	// load balances over the pods referenced by the ScaleTargetRef.
	ServiceName string `json:"serviceName"`

	// The application-layer protocol. Matches `ProtocolType` inferred from the revision spec.
	ProtocolType net.ProtocolType
}

const (
	// PodAutoscalerConditionReady is set when the revision is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	PodAutoscalerConditionReady = duckv1alpha1.ConditionReady
	// PodAutoscalerConditionActive is set when the PodAutoscaler's ScaleTargetRef is receiving traffic.
	PodAutoscalerConditionActive duckv1alpha1.ConditionType = "Active"
)

// PodAutoscalerStatus communicates the observed state of the PodAutoscaler (from the controller).
type PodAutoscalerStatus duckv1alpha1.Status

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodAutoscalerList is a list of PodAutoscaler resources
type PodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PodAutoscaler `json:"items"`
}
