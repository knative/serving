/*
Copyright 2019 The Knative Authors

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Configuration represents the "floating HEAD" of a linear history of Revisions.
// Users create new Revisions by updating the Configuration's spec.
// The "latest created" revision's name is available under status, as is the
// "latest ready" revision's name.
// See also: https://github.com/knative/serving/blob/master/docs/spec/overview.md#configuration
type Configuration struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ConfigurationSpec `json:"spec,omitempty"`

	// +optional
	Status ConfigurationStatus `json:"status,omitempty"`
}

// Verify that Configuration adheres to the appropriate interfaces.
var (
	// Check that Configuration may be validated and defaulted.
	_ apis.Validatable = (*Configuration)(nil)
	_ apis.Defaultable = (*Configuration)(nil)

	// Check that Configuration can be converted to higher versions.
	_ apis.Convertible = (*Configuration)(nil)

	// Check that we can create OwnerReferences to a Configuration.
	_ kmeta.OwnerRefable = (*Configuration)(nil)

	// Check that the type conforms to the duck Knative Resource shape.
	_ duckv1.KRShaped = (*Configuration)(nil)
)

// ConfigurationSpec holds the desired state of the Configuration (from the client).
type ConfigurationSpec struct {
	// Template holds the latest specification for the Revision to be stamped out.
	// +optional
	Template RevisionTemplateSpec `json:"template"`
}

const (
	// ConfigurationConditionReady is set when the configuration's latest
	// underlying revision has reported readiness.
	ConfigurationConditionReady = apis.ConditionReady
)

func IsConfigurationCondition(t apis.ConditionType) bool {
	return t == ConfigurationConditionReady
}

// ConfigurationStatusFields holds the fields of Configuration's status that
// are not generally shared.  This is defined separately and inlined so that
// other types can readily consume these fields via duck typing.
type ConfigurationStatusFields struct {
	// LatestReadyRevisionName holds the name of the latest Revision stamped out
	// from this Configuration that has had its "Ready" condition become "True".
	// +optional
	LatestReadyRevisionName string `json:"latestReadyRevisionName,omitempty"`

	// LatestCreatedRevisionName is the last revision that was created from this
	// Configuration. It might not be ready yet, for that use LatestReadyRevisionName.
	// +optional
	LatestCreatedRevisionName string `json:"latestCreatedRevisionName,omitempty"`
}

// ConfigurationStatus communicates the observed state of the Configuration (from the controller).
type ConfigurationStatus struct {
	duckv1.Status `json:",inline"`

	ConfigurationStatusFields `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ConfigurationList is a list of Configuration resources
type ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Configuration `json:"items"`
}

// GetStatus retrieves the status of the Configuration. Implements the KRShaped interface.
func (t *Configuration) GetStatus() *duckv1.Status {
	return &t.Status.Status
}
