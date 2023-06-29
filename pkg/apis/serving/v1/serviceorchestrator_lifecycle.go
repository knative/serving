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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
)

var serviceOrchestratorCondSet = apis.NewLivingConditionSet(
	ServiceOrchestratorStageReady,
	ServiceOrchestratorLastStageComplete,
)

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*ServiceOrchestrator) GetConditionSet() apis.ConditionSet {
	return serviceOrchestratorCondSet
}

// GetGroupVersionKind returns the GroupVersionKind.
func (*ServiceOrchestrator) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("ServiceOrchestrator")
}

// IsReady returns true if the Status condition ServiceOrchestratorConditionReady
// is true and the latest spec has been observed.
func (c *ServiceOrchestrator) IsReady() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorConditionReady).IsTrue()
}

// IsFailed returns true if the resource has observed
// the latest generation and ready is false.
func (c *ServiceOrchestrator) IsFailed() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorConditionReady).IsFalse()
}

func (c *ServiceOrchestrator) IsInProgress() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorConditionReady).IsUnknown()
}

func (c *ServiceOrchestrator) IsStageInProgress() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorStageReady).IsUnknown()
}

func (c *ServiceOrchestrator) IsStageReady() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorStageReady).IsTrue()
}

func (c *ServiceOrchestrator) IsStageFailed() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorStageReady).IsFalse()
}

func (c *ServiceOrchestrator) IsStageScaleUpReady() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorStageScaleUpReady).IsTrue()
}

func (c *ServiceOrchestrator) IsStageScaleUpInProgress() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ServiceOrchestratorStageScaleUpReady).IsUnknown()
}

// InitializeConditions sets the initial values to the conditions.
func (cs *ServiceOrchestratorStatus) InitializeConditions() {
	serviceOrchestratorCondSet.Manage(cs).InitializeConditions()
}

// MarkStageRevisionFailed marks the ServiceOrchestratorStageReady condition to
// indicate that the revision rollout failed for the current stage.
func (cs *ServiceOrchestratorStatus) MarkStageRevisionFailed(message string) {
	serviceOrchestratorCondSet.Manage(cs).MarkFalse(
		ServiceOrchestratorStageReady,
		"StageRevisionRolloutFailed",
		"The rollout of the current stage failed with message: %s.", message)
	serviceOrchestratorCondSet.Manage(cs).MarkFalse(
		ServiceOrchestratorLastStageComplete,
		"RevisionRolloutFailed",
		"The rollout of the current stage failed with message: %s.", message)
}

// MarkStageRevisionReady marks the ServiceOrchestratorStageReady condition to
// indicate that the revision rollout succeeded for the current stage.
func (cs *ServiceOrchestratorStatus) MarkStageRevisionReady() {
	serviceOrchestratorCondSet.Manage(cs).MarkTrue(ServiceOrchestratorStageReady)
}

func (cs *ServiceOrchestratorStatus) MarkStageRevisionScaleUpReady() {
	serviceOrchestratorCondSet.Manage(cs).MarkTrue(ServiceOrchestratorStageScaleUpReady)
}

func (cs *ServiceOrchestratorStatus) MarkStageRevisionScaleDownReady() {
	serviceOrchestratorCondSet.Manage(cs).MarkTrue(ServiceOrchestratorStageScaleDownReady)
}

// MarkLastStageRevisionComplete marks the ServiceOrchestratorLastStageComplete condition to
// indicate that the revision rollout succeeded for the last stage.
func (cs *ServiceOrchestratorStatus) MarkLastStageRevisionComplete() {
	serviceOrchestratorCondSet.Manage(cs).MarkTrue(ServiceOrchestratorLastStageComplete)
}

func (cs *ServiceOrchestratorStatus) MarkStageRevisionInProgress(reason, message string) {
	serviceOrchestratorCondSet.Manage(cs).MarkUnknown(ServiceOrchestratorStageReady, reason, message)
}

func (cs *ServiceOrchestratorStatus) MarkStageRevisionScaleDownInProgress(reason, message string) {
	serviceOrchestratorCondSet.Manage(cs).MarkUnknown(ServiceOrchestratorStageScaleDownReady, reason, message)
}

func (cs *ServiceOrchestratorStatus) MarkStageRevisionScaleUpInProgress(reason, message string) {
	serviceOrchestratorCondSet.Manage(cs).MarkUnknown(ServiceOrchestratorStageScaleUpReady, reason, message)
}
