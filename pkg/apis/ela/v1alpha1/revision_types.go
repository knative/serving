/*
Copyright 2018 Google LLC.

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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Revision is an immutable snapshot of code and configuration.
// See also: https://github.com/elafros/elafros/blob/master/docs/spec/overview.md#revision
type Revision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Revision (from the client).
	Spec RevisionSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Revision (from the controller).
	Status RevisionStatus `json:"status,omitempty"`
}

// RevisionTemplateSpec describes the data a revision should have when created from a template.
// Based on: https://github.com/kubernetes/api/blob/e771f807/core/v1/types.go#L3179-L3190
type RevisionTemplateSpec struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RevisionSpec `json:"spec,omitempty"`
}

// RevisionServingStateType is an enumeration of the levels of serving readiness into which the Revision may be put.
// See also: https://github.com/elafros/elafros/blob/master/docs/spec/errors.md#error-conditions-and-reporting
type RevisionServingStateType string

const (
	// The revision is ready to serve traffic. It should have Kubernetes
	// resources, and the Istio route should be pointed to the given resources.
	RevisionServingStateActive RevisionServingStateType = "Active"
	// The revision is not currently serving traffic, but could be made to serve
	// traffic quickly. It should have Kubernetes resources, but the Istio route
	// should be pointed to the activator.
	RevisionServingStateReserve RevisionServingStateType = "Reserve"
	// The revision has been decommissioned and is not needed to serve traffic
	// anymore. It should not have any Istio routes or Kubernetes resources.
	// A Revision may be brought out of retirement, but it may take longer.
	RevisionServingStateRetired RevisionServingStateType = "Retired"
)

// RevisionSpec holds the desired state of the Revision (from the client).
type RevisionSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64 `json:"generation,omitempty"`

	// ServingState holds a value describing the desired state the Kubernetes
	// resources should be in for this Revision.
	// Users may not specify this when creating a revision. It is expected
	// that the system will manipulate this based on routability and load.
	ServingState RevisionServingStateType `json:"servingState"`

	// ServiceAccountName holds the name of the Kubernetes service account
	// as which the underlying K8s resources should be run. If unspecified
	// this will default to the "default" service account for the namespace
	// in which the Revision exists.
	// This may be used to provide access to private container images by
	// following: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account
	// TODO(ZhiminXiang): verify the corresponding service account exists.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// BuildName optionally holds the name of the Build responsible for
	// producing the container image for tis Revision.
	BuildName string `json:"buildName,omitempty"`

	// Container defines the unit of execution for this Revision.
	// In the context of a Revision, we disallow a number of the fields of
	// this Container, including: name, resources, ports, and volumeMounts.
	// TODO(mattmoor): Link to the runtime contract tracked by:
	// https://github.com/elafros/elafros/issues/627
	Container corev1.Container `json:"container,omitempty"`
}

// RevisionConditionType is used to communicate the status of the reconciliation process.
// See also: https://github.com/elafros/elafros/blob/master/docs/spec/errors.md#error-conditions-and-reporting
type RevisionConditionType string

const (
	// RevisionConditionReady is set when the revision is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	RevisionConditionReady RevisionConditionType = "Ready"
	// RevisionConditionFailed is set when the revision readiness check exceeds 3.
	RevisionConditionFailed RevisionConditionType = "Failed"
	// RevisionConditionBuildComplete is set when the revision has an associated build
	// and is marked True if/once the Build has completed succesfully.
	RevisionConditionBuildComplete RevisionConditionType = "BuildComplete"
	// RevisionConditionBuildFailed is set when the revision has an associated build
	// that has failed for some reason.
	RevisionConditionBuildFailed RevisionConditionType = "BuildFailed"
)

// RevisionCondition defines a readiness condition for a Revision.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type RevisionCondition struct {
	Type RevisionConditionType `json:"type" description:"type of Revision condition"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// RevisionStatus communicates the observed state of the Revision (from the controller).
type RevisionStatus struct {

	// ServiceName holds the name of a core Kubernetes Service resource that
	// load balances over the pods backing this Revision. When the Revision
	// is Active, this service would be an appropriate ingress target for
	// targeting the revision.
	ServiceName string `json:"serviceName,omitempty"`

	// Conditions communicates information about ongoing/complete
	// reconciliation processes that bring the "spec" inline with the observed
	// state of the world.
	Conditions []RevisionCondition `json:"conditions,omitempty"`

	// ObservedGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec and create the Revision.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RevisionList is a list of Revision resources
type RevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Revision `json:"items"`
}

func (r *Revision) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *Revision) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *Revision) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

// IsReady looks at the conditions and if the Status has a condition
// RevisionConditionReady returns true if ConditionStatus is True
func (rs *RevisionStatus) IsReady() bool {
	if c := rs.GetCondition(RevisionConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

// IsFailed looks at the conditions and if the Status has a condition
// RevisionConditionFailed returns true if ConditionStatus is True
func (rs *RevisionStatus) IsFailed() bool {
	if c := rs.GetCondition(RevisionConditionFailed); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

func (rs *RevisionStatus) GetCondition(t RevisionConditionType) *RevisionCondition {
	for _, cond := range rs.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (rs *RevisionStatus) SetCondition(new *RevisionCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []RevisionCondition
	for _, cond := range rs.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	conditions = append(conditions, *new)
	rs.Conditions = conditions
}

func (rs *RevisionStatus) RemoveCondition(t RevisionConditionType) {
	var conditions []RevisionCondition
	for _, cond := range rs.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	rs.Conditions = conditions
}
