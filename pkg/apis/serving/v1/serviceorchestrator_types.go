/*
Copyright 2023 The Knative Authors

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

// ServiceOrchestrator represents the orchestractor to launch the new revision and direct the traffic
// in an incremental way.
type ServiceOrchestrator struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ServiceOrchestratorSpec `json:"spec,omitempty"`

	// +optional
	Status ServiceOrchestratorStatus `json:"status,omitempty"`
}

// Verify that Configuration adheres to the appropriate interfaces.
var (
	// Check that Configuration may be validated and defaulted.
	//_ apis.Validatable = (*ServiceOrchestrator)(nil)
	_ apis.Defaultable = (*ServiceOrchestrator)(nil)

	// Check that Configuration can be converted to higher versions.
	_ apis.Convertible = (*ServiceOrchestrator)(nil)

	// Check that we can create OwnerReferences to a Configuration.
	_ kmeta.OwnerRefable = (*ServiceOrchestrator)(nil)

	// Check that the type conforms to the duck Knative Resource shape.
	_ duckv1.KRShaped = (*ServiceOrchestrator)(nil)
)

// RevisionTarget holds the information of the revision for the current stage.
type RevisionTarget struct {
	// RevisionName indicates RevisionName.
	// +optional
	RevisionName string `json:"revisionName,omitempty"`

	// Direction indicates up or down.
	// +optional
	Direction string `json:"direction,omitempty"`

	// TargetReplicas indicates an estimated number of replicas.
	// +optional
	TargetReplicas *int32 `json:"targetReplicas,omitempty"`

	// LatestRevision indicates whether it is the last revision or not.
	// +optional
	LatestRevision *bool `json:"latestRevision,omitempty"`

	// Percent indicates that percentage based routing should be used and
	// the value indicates the percent of traffic that is be routed to this
	// Revision or Configuration. `0` (zero) mean no traffic, `100` means all
	// traffic.
	// When percentage based routing is being used the follow rules apply:
	// - the sum of all percent values must equal 100
	// - when not specified, the implied value for `percent` is zero for
	//   that particular Revision or Configuration
	// +optional
	Percent *int64 `json:"percent,omitempty"`

	// MinScale sets the lower bound for the number of the replicas.
	// +optional
	MinScale *int32 `json:"minScale,omitempty"`

	// MaxScale sets the upper bound for the number of the replicas.
	// +optional
	MaxScale *int32 `json:"maxScale,omitempty"`
}

type StageTarget struct {
	// StageTraffic holds the configured traffic distribution fot the current stage.
	// These entries will always contain RevisionName references.
	// When ConfigurationName appears in the spec, this will hold the
	// LatestReadyRevisionName that we last observed.
	// +optional
	StageRevisionTarget []RevisionTarget `json:"stageRevisionTarget,omitempty"`

	// TargetFinishTime indicates target time to complete this target.
	// +optional
	TargetFinishTime apis.VolatileTime `json:"targetFinishTime,omitempty"`
}

// ServiceOrchestratorSpec holds the desired state of the Configuration (from the client).
type ServiceOrchestratorSpec struct {
	StageTarget `json:",inline"`

	// Traffic holds the configured traffic distribution.
	// These entries will always contain RevisionName references.
	// When ConfigurationName appears in the spec, this will hold the
	// LatestReadyRevisionName that we last observed.
	// +optional
	RevisionTarget []RevisionTarget `json:"revisionTarget,omitempty"`

	// Traffic holds the configured traffic distribution.
	// These entries will always contain RevisionName references.
	// When ConfigurationName appears in the spec, this will hold the
	// LatestReadyRevisionName that we last observed.
	// +optional
	InitialRevisionStatus []RevisionTarget `json:"initialRevisionStatus,omitempty"`
}

const (
	// ServiceOrchestratorConditionReady is set when the configuration's latest
	// underlying revision has reported readiness.
	ServiceOrchestratorConditionReady = apis.ConditionReady

	// ServiceOrchestratorStageReady is set to False when the
	// service is not configured properly or has no available
	// backends ready to receive traffic.
	ServiceOrchestratorStageReady apis.ConditionType = "StageTrafficReady"

	// ServiceOrchestratorLastStageComplete is set to False when the
	// Ingress fails to become Ready.
	ServiceOrchestratorLastStageComplete apis.ConditionType = "LastStageTrafficReady"

	ServiceOrchestratorStageScaleUpReady apis.ConditionType = "StageTrafficScaleUpReady"

	ServiceOrchestratorStageScaleDownReady apis.ConditionType = "StageTrafficScaleDownReady"

	PodAutoscalerStageReady apis.ConditionType = "PodAutoscalerStageReady"
)

// IsServiceOrchestratorCondition returns true if the given ConditionType is a ServiceOrchestratorCondition.
func IsServiceOrchestratorCondition(t apis.ConditionType) bool {
	return t == ServiceOrchestratorConditionReady
}

// ServiceOrchestratorStatusFields holds the fields of Configuration's status that
// are not generally shared.  This is defined separately and inlined so that
// other types can readily consume these fields via duck typing.
type ServiceOrchestratorStatusFields struct {
	// StageRevisionStatus holds the traffic split.
	// +optional
	StageRevisionStatus []RevisionTarget `json:"stageRevisionStatus,omitempty"`
}

// ServiceOrchestratorStatus communicates the observed state of the Configuration (from the controller).
type ServiceOrchestratorStatus struct {
	duckv1.Status `json:",inline"`

	ServiceOrchestratorStatusFields `json:",inline"`
}

func (t *ServiceOrchestratorStatus) SetStageRevisionStatus(stageRevisionStatus []RevisionTarget) {
	t.StageRevisionStatus = stageRevisionStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceOrchestratorList is a list of Configuration resources
type ServiceOrchestratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceOrchestrator `json:"items"`
}

// GetStatus retrieves the status of the Configuration. Implements the KRShaped interface.
func (t *ServiceOrchestrator) GetStatusSO() *ServiceOrchestratorStatus {
	return &t.Status
}

func (t *ServiceOrchestrator) GetStatus() *duckv1.Status {
	return &t.Status.Status
}
