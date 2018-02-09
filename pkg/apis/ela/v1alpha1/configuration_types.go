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

	build "github.com/google/elafros/pkg/apis/cloudbuild/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Configuration
type Configuration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigurationSpec   `json:"spec,omitempty"`
	Status ConfigurationStatus `json:"status,omitempty"`
}

// ConfigurationSpec defines the desired state of Configuration
type ConfigurationSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64            `json:"generation,omitempty"`
	Build      *build.BuildSpec `json:"build,omitempty"`
	Template   Revision         `json:"template"`
}

// ConfigurationStatus defines the observed state of Configuration
type ConfigurationStatus struct {
	// Latest revision that is ready.
	Latest string `json:"latest,omitempty"`
	// LatestCreated is the last revision that has been created, it might not be
	// ready yet however. Hence we just keep track of it so that when it's ready
	// it will get moved to Latest.
	LatestCreated string `json:"latestCreated,omitempty"`
	// ReconciledGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The reconciled generation is updated
	// even if the controller failed to process the spec and create the Revision.
	ReconciledGeneration int64 `json:"reconciledGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ConfigurationList is a list of Configuration resources
type ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Configuration `json:"items"`
}

func (r *Configuration) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *Configuration) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *Configuration) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

// TODO(mattmoor): Once Configuration has Conditions
// func (rts *ConfigurationStatus) SetCondition(new *ConfigurationCondition) {
// 	if new == nil {
// 		return
// 	}

// 	t := new.Type
// 	var conditions []ConfigurationCondition
// 	for _, cond := range rts.Conditions {
// 		if cond.Type != t {
// 			conditions = append(conditions, cond)
// 		}
// 	}
// 	conditions = append(conditions, *new)
// 	rts.Conditions = conditions
// }

// func (rts *ConfigurationStatus) RemoveCondition(t string) {
// 	var conditions []ConfigurationCondition
// 	for _, cond := range rts.Conditions {
// 		if cond.Type != t {
// 			conditions = append(conditions, cond)
// 		}
// 	}
// 	rts.Conditions = conditions
// }
