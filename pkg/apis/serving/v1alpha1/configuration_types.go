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
	"reflect"
	"sort"
	"time"

	build "github.com/knative/build/pkg/apis/build/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
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

// ConfigurationConditionType is used to communicate the status of the reconciliation process.
// See also: https://github.com/knative/serving/blob/master/docs/spec/errors.md#error-conditions-and-reporting
type ConfigurationConditionType string

const (
	// ConfigurationConditionReady is set when the configuration's latest
	// underlying revision has reported readiness.
	ConfigurationConditionReady ConfigurationConditionType = "Ready"
)

// ConfigurationCondition defines a readiness condition for a Configuration.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type ConfigurationCondition struct {
	Type ConfigurationConditionType `json:"type" description:"type of Configuration condition"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	// We use VolatileTime in place of metav1.Time to exclude this from creating equality.Semantic
	// differences (all other things held constant).
	LastTransitionTime VolatileTime `json:"lastTransitionTime,omitempty" description:"last time the condition transit from one status to another"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ConfigurationStatus communicates the observed state of the Configuration (from the controller).
type ConfigurationStatus struct {
	// Conditions communicates information about ongoing/complete
	// reconciliation processes that bring the "spec" inline with the observed
	// state of the world.
	// +optional
	Conditions []ConfigurationCondition `json:"conditions,omitempty"`

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

// IsReady looks at the conditions on the ConfigurationStatus.
// ConfigurationConditionReady returns true if ConditionStatus is True
func (cs *ConfigurationStatus) IsReady() bool {
	if c := cs.GetCondition(ConfigurationConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

// IsLatestReadyRevisionNameUpToDate returns true if the Configuration is ready
// and LatestCreateRevisionName is equal to LatestReadyRevisionName. Otherwise
// it returns false.
func (cs *ConfigurationStatus) IsLatestReadyRevisionNameUpToDate() bool {
	return cs.IsReady() &&
		cs.LatestCreatedRevisionName == cs.LatestReadyRevisionName
}

func (config *ConfigurationStatus) GetCondition(t ConfigurationConditionType) *ConfigurationCondition {
	for _, cond := range config.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (cs *ConfigurationStatus) setCondition(new *ConfigurationCondition) {
	if new == nil {
		return
	}
	t := new.Type
	var conditions []ConfigurationCondition
	for _, cond := range cs.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		} else {
			// If we'd only update the LastTransitionTime, then return.
			new.LastTransitionTime = cond.LastTransitionTime
			if reflect.DeepEqual(new, &cond) {
				return
			}
		}
	}
	new.LastTransitionTime = VolatileTime{metav1.NewTime(time.Now())}
	conditions = append(conditions, *new)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	cs.Conditions = conditions
}

func (cs *ConfigurationStatus) InitializeConditions() {
	for _, cond := range []ConfigurationConditionType{
		ConfigurationConditionReady,
	} {
		if rc := cs.GetCondition(cond); rc == nil {
			cs.setCondition(&ConfigurationCondition{
				Type:   cond,
				Status: corev1.ConditionUnknown,
			})
		}
	}
}

func (cs *ConfigurationStatus) SetLatestCreatedRevisionName(name string) {
	cs.LatestCreatedRevisionName = name
	if cs.LatestReadyRevisionName != name {
		cs.setCondition(&ConfigurationCondition{
			Type:   ConfigurationConditionReady,
			Status: corev1.ConditionUnknown,
		})
	}
}

func (cs *ConfigurationStatus) SetLatestReadyRevisionName(name string) {
	cs.LatestReadyRevisionName = name
	for _, cond := range []ConfigurationConditionType{
		ConfigurationConditionReady,
	} {
		cs.setCondition(&ConfigurationCondition{
			Type:   cond,
			Status: corev1.ConditionTrue,
		})
	}
}

func (cs *ConfigurationStatus) MarkLatestCreatedFailed(name, message string) {
	cct := []ConfigurationConditionType{ConfigurationConditionReady}
	if cs.LatestReadyRevisionName == "" {
		cct = append(cct, ConfigurationConditionReady)
	}
	for _, cond := range cct {
		cs.setCondition(&ConfigurationCondition{
			Type:    cond,
			Status:  corev1.ConditionFalse,
			Reason:  "RevisionFailed",
			Message: fmt.Sprintf("Revision %q failed with message: %q.", name, message),
		})
	}
}

func (cs *ConfigurationStatus) MarkRevisionCreationFailed(message string) {
	cs.setCondition(&ConfigurationCondition{
		Type:    ConfigurationConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  "RevisionFailed",
		Message: fmt.Sprintf("Revision creation failed with message: %q.", message),
	})
}

func (cs *ConfigurationStatus) MarkLatestReadyDeleted() {
	cct := []ConfigurationConditionType{ConfigurationConditionReady}
	for _, cond := range cct {
		cs.setCondition(&ConfigurationCondition{
			Type:    cond,
			Status:  corev1.ConditionFalse,
			Reason:  "RevisionDeleted",
			Message: fmt.Sprintf("Revision %q was deleted.", cs.LatestReadyRevisionName),
		})
	}
	cs.LatestReadyRevisionName = ""
}
