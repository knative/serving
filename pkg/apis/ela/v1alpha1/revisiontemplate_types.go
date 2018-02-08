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

	build "github.com/google/elafros/pkg/apis/cloudbuild/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RevisionTemplate
type RevisionTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RevisionTemplateSpec   `json:"spec,omitempty"`
	Status RevisionTemplateStatus `json:"status,omitempty"`
}

// RevisionTemplateSpec defines the desired state of RevisionTemplate
type RevisionTemplateSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64            `json:"generation,omitempty"`
	Build      *build.BuildSpec `json:"build,omitempty"`
	Template   Revision         `json:"template"`
}

// RevisionTemplateConditionType represents an RevisionTemplate condition value
type RevisionTemplateConditionType string

const (
	// RevisionTemplateConditionReady is set when the revisiontemplate has
	// successfully created a Revision
	RevisionTemplateConditionReady RevisionTemplateConditionType = "Ready"
)

// RevisionTemplateCondition defines a readiness condition for a RevisionTemplate.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type RevisionTemplateCondition struct {
	Type RevisionTemplateConditionType `json:"type" description:"type of RevisionTemplate condition"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// RevisionTemplateStatus defines the observed state of RevisionTemplate
type RevisionTemplateStatus struct {
	// Latest revision that is ready.
	Latest string `json:"latest,omitempty"`
	// LatestCreated is the last revision that has been created, it might not be
	// ready yet however. Hence we just keep track of it so that when it's ready
	// it will get moved to Latest.
	LatestCreated string `json:"latestCreated,omitempty"`
	// ReconciledGeneration is the 'Generation' of the RevisionTemplate that
	// was last processed by the controller. The reconciled generation is updated
	// even if the controller failed to process the spec and create the Revision.
	ReconciledGeneration int64 `json:"reconciledGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RevisionTemplateList is a list of RevisionTemplate resources
type RevisionTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []RevisionTemplate `json:"items"`
}

func (r *RevisionTemplate) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *RevisionTemplate) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *RevisionTemplate) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

// TODO(mattmoor): Once RevisionTemplate has Conditions
// func (rts *RevisionTemplateStatus) SetCondition(t string, new *RevisionTemplateCondition) {
// 	var conditions []RevisionTemplateCondition
// 	for _, cond := range rts.Conditions {
// 		if cond.Type != t {
// 			conditions = append(conditions, cond)
// 		}
// 	}
// 	if new != nil {
// 		conditions = append(conditions, *new)
// 	}
// 	rts.Conditions = conditions
// }
//
// func (rts *RevisionTemplateStatus) RemoveCondition(t string) {
// 	rts.SetCondition(t, nil)
// }
