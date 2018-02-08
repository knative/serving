/*
Copyright 2017 The Kubernetes Authors.

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

// Revision
type Revision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RevisionSpec   `json:"spec,omitempty"`
	Status RevisionStatus `json:"status,omitempty"`
}

type FunctionSpec struct {
	Entrypoint string `json:"entrypoint"`
	//	Timeout    *metav1.Duration `json:"timeoutDuration,omitempty"`
	Timeout metav1.Duration `json:"timeoutDuration,omitempty"`
}

type AppSpec struct {
	// TODO: What goes here, not in the Ela API spec
	Image string `json:"image,omitempty"`
}

type ContainerSpec struct {
	Image string `json:"image,omitempty"`
}

// RevisionSpec defines the desired state of Revision
type RevisionSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64 `json:"generation,omitempty"`

	// TODO(vaikas): I think we still need this?
	// Service this is part of. Points to the Service in the namespace
	Service string `json:"service"`

	// TODO(vaikas): I think we still need this?
	// Active says whether k8s resources should be created for this deployment.
	// When true, controller will make the resources and when false, will delete them
	// as necessary
	Active bool `json:"active"`

	// The name of the build that is producing the container image that we are deploying.
	BuildName string `json:"buildName,omitempty"`

	// TODO(mattmoor): Remove these, and type definitions above.
	FunctionSpec *FunctionSpec `json:"functionSpec,omitempty"`
	AppSpec      *AppSpec      `json:"appSpec,omitempty"`

	// TODO(mattmoor): Change to corev1.Container
	ContainerSpec *ContainerSpec `json:"containerSpec,omitempty"`

	// List of environment variables that will be passed to the app container.
	// TODO: Add merge strategy for this similar to the EnvVar list on the
	// Container type.
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// RevisionConditionType represents an Revision condition value
type RevisionConditionType string

const (
	// RevisionConditionReady is set when the revision is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	RevisionConditionReady RevisionConditionType = "Ready"
	// RevisionConditionBuildComplete is set when the revision has an associated build
	// and is marked True if/once the Build has completed succesfully.
	RevisionConditionBuildComplete RevisionConditionType = "BuildComplete"
	// RevisionConditionBuildFailed is set when the revision has an associated build
	// that has failed for some reason.
	RevisionConditionBuildFailed RevisionConditionType = "BuildFailed"
)

// RevisionCondition defines a readiness condition for a ElaDeployment.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type RevisionCondition struct {
	Type RevisionConditionType `json:"type" description:"type of ElaDeployment condition"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// RevisionStatus defines the observed state of Revision
type RevisionStatus struct {
	// This is the k8s name of the service that represents this revision.
	// We expose this to ensure that we can easily route to it from
	// ElaService.
	ServiceName string              `json:"serviceName,omitempty"`
	Conditions  []RevisionCondition `json:"conditions,omitempty"`
	// ReconciledGeneration is the 'Generation' of the RevisionTemplate that
	// was last processed by the controller. The reconciled generation is updated
	// even if the controller failed to process the spec and create the Revision.
	ReconciledGeneration int64 `json:"reconciledGeneration,omitempty"`
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

// IsRevisionReady looks at the conditions and if the Status has a condition
// RevisionConditionReady returns true if ConditionStatus is True
func (rs *RevisionStatus) IsReady() bool {
	if c := rs.GetCondition(RevisionConditionReady); c != nil {
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
