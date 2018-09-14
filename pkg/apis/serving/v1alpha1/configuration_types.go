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
	"encoding/json"
	"fmt"

	build "github.com/knative/build/pkg/apis/build/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/kmeta"
	sapis "github.com/knative/serving/pkg/apis"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Configuration represents the "floating HEAD" of a linear history of Revisions,
// and optionally how the containers those revisions reference are built.
// Users create new Revisions by updating the Configuration's spec.
// The "latest created" revision's name is available under status, as is the
// "latest ready" revision's name.
// See also: https://github.com/knative/serving/blob/master/docs/spec/overview.md#configuration
type Configuration struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Configuration (from the client).
	// +optional
	Spec ConfigurationSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Configuration (from the controller).
	// +optional
	Status ConfigurationStatus `json:"status,omitempty"`
}

// Check that Configuration may be validated and defaulted.
var _ apis.Validatable = (*Configuration)(nil)
var _ apis.Defaultable = (*Configuration)(nil)

// Check that we can create OwnerReferences to a Configuration.
var _ kmeta.OwnerRefable = (*Configuration)(nil)

// Check that ConfigurationStatus may have it's conditions managed.
var _ sapis.Conditions = (*ConfigurationStatus)(nil)

// ConfigurationSpec holds the desired state of the Configuration (from the client).
type ConfigurationSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// Build optionally holds the specification for the build to
	// perform to produce the Revision's container image.
	// +optional
	Build *build.BuildSpec `json:"build,omitempty"`

	// RevisionTemplate holds the latest specification for the Revision to
	// be stamped out. If a Build specification is provided, then the
	// RevisionTemplate's BuildName field will be populated with the name of
	// the Build object created to produce the container for the Revision.
	// +optional
	RevisionTemplate RevisionTemplateSpec `json:"revisionTemplate"`
}

const (
	// ConfigurationConditionReady is set when the configuration's latest
	// underlying revision has reported readiness.
	ConfigurationConditionReady = sapis.ConditionReady
)

var confCondSet = sapis.NewOngoingConditionSet()

// ConfigurationStatus communicates the observed state of the Configuration (from the controller).
type ConfigurationStatus struct {
	// Conditions communicates information about ongoing/complete
	// reconciliation processes that bring the "spec" inline with the observed
	// state of the world.
	// +optional
	Conditions []sapis.Condition `json:"conditions,omitempty"`

	// LatestReadyRevisionName holds the name of the latest Revision stamped out
	// from this Configuration that has had its "Ready" condition become "True".
	// +optional
	LatestReadyRevisionName string `json:"latestReadyRevisionName,omitempty"`

	// LatestCreatedRevisionName is the last revision that was created from this
	// Configuration. It might not be ready yet, for that use LatestReadyRevisionName.
	// +optional
	LatestCreatedRevisionName string `json:"latestCreatedRevisionName,omitempty"`

	// ObservedGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec and create the Revision.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
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

func (r *Configuration) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Configuration")
}

// IsReady looks at the conditions to see if they are happy.
func (cs *ConfigurationStatus) IsReady() bool {
	return confCondSet.Using(cs).IsHappy()
}

// IsLatestReadyRevisionNameUpToDate returns true if the Configuration is ready
// and LatestCreateRevisionName is equal to LatestReadyRevisionName. Otherwise
// it returns false.
func (cs *ConfigurationStatus) IsLatestReadyRevisionNameUpToDate() bool {
	return cs.IsReady() &&
		cs.LatestCreatedRevisionName == cs.LatestReadyRevisionName
}

func (cs *ConfigurationStatus) GetCondition(t sapis.ConditionType) *sapis.Condition {
	return confCondSet.Using(cs).GetCondition(t)
}

// This is kept for unit test integration.
func (cs *ConfigurationStatus) setCondition(new *sapis.Condition) {
	if new != nil {
		confCondSet.Using(cs).SetCondition(*new)
	}
}

func (cs *ConfigurationStatus) InitializeConditions() {
	confCondSet.Using(cs).InitializeConditions()
}

func (cs *ConfigurationStatus) SetLatestCreatedRevisionName(name string) {
	cs.LatestCreatedRevisionName = name
	if cs.LatestReadyRevisionName != name {
		confCondSet.Using(cs).MarkUnknown(
			ConfigurationConditionReady,
			"LatestReadyRevisionNameMismatch",
			fmt.Sprintf("%q != %q", cs.LatestReadyRevisionName, name))
	}
}

func (cs *ConfigurationStatus) SetLatestReadyRevisionName(name string) {
	cs.LatestReadyRevisionName = name
	confCondSet.Using(cs).MarkTrue(ConfigurationConditionReady)
}

func (cs *ConfigurationStatus) MarkLatestCreatedFailed(name, message string) {
	confCondSet.Using(cs).MarkFalse(
		ConfigurationConditionReady,
		"RevisionFailed",
		fmt.Sprintf("Revision %q failed with message: %q.", name, message))
}

func (cs *ConfigurationStatus) MarkRevisionCreationFailed(message string) {
	confCondSet.Using(cs).MarkFalse(
		ConfigurationConditionReady,
		"RevisionFailed",
		fmt.Sprintf("Revision creation failed with message: %q.", message))
}

func (cs *ConfigurationStatus) MarkLatestReadyDeleted() {
	confCondSet.Using(cs).MarkFalse(
		ConfigurationConditionReady,
		"RevisionDeleted",
		fmt.Sprintf("Revision %q was deleted.", cs.LatestReadyRevisionName))
}

// GetConditions returns the Conditions array. This enables generic handling of
// conditions by implementing the sapis.Conditions interface.
func (cs *ConfigurationStatus) GetConditions() []sapis.Condition {
	return cs.Conditions
}

// SetConditions sets the Conditions array. This enables generic handling of
// conditions by implementing the sapis.Conditions interface.
func (cs *ConfigurationStatus) SetConditions(conditions []sapis.Condition) {
	cs.Conditions = conditions
}
