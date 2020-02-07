/*
Copyright 2019 The Knative Authors.

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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
)

const (
	// ReasonContainerMissing defines the reason for marking container healthiness status
	// as false if the a container image for the revision is missing.
	ReasonContainerMissing = "ContainerMissing"

	// ReasonDeploying defines the reason for marking revision availability status as
	// unknown if the revision is still deploying.
	ReasonDeploying = "Deploying"

	// ReasonNotOwned defines the reason for marking revision availability status as
	// false due to resource ownership issues.
	ReasonNotOwned = "NotOwned"

	// ReasonProgressDeadlineExceeded defines the reason for marking revision availability
	// status as false if progress has exceeded the deadline.
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
)

var revisionCondSet = apis.NewLivingConditionSet(
	RevisionConditionResourcesAvailable,
	RevisionConditionContainerHealthy,
)

// GetGroupVersionKind returns the GroupVersionKind.
func (r *Revision) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Revision")
}

// IsReady returns if the revision is ready to serve the requested configuration.
func (rs *RevisionStatus) IsReady() bool {
	return revisionCondSet.Manage(rs).IsHappy()
}

// GetContainerConcurrency returns the container concurrency. If
// container concurrency is not set, the default value will be returned.
// We use the original default (0) here for backwards compatibility.
// Previous versions of Knative equated unspecified and zero, so to avoid
// changing the value used by Revisions with unspecified values when a different
// default is configured, we use the original default instead of the configured
// default to remain safe across upgrades.
func (rs *RevisionSpec) GetContainerConcurrency() int64 {
	if rs.ContainerConcurrency == nil {
		return config.DefaultContainerConcurrency
	}
	return *rs.ContainerConcurrency
}

// InitializeConditions sets the initial values to the conditions.
func (rs *RevisionStatus) InitializeConditions() {
	revisionCondSet.Manage(rs).InitializeConditions()
}

// MarkActiveTrue marks Active status on revision as True
func (rs *RevisionStatus) MarkActiveTrue() {
	revisionCondSet.Manage(rs).MarkTrue(RevisionConditionActive)
}

// MarkActiveFalse marks Active status on revision as False
func (rs *RevisionStatus) MarkActiveFalse(reason, message string) {
	revisionCondSet.Manage(rs).MarkFalse(RevisionConditionActive, reason, message)
}

// MarkActiveUnknown marks Active status on revision as Unknown
func (rs *RevisionStatus) MarkActiveUnknown(reason, message string) {
	revisionCondSet.Manage(rs).MarkUnknown(RevisionConditionActive, reason, message)
}

// MarkContainerHealthyTrue marks ContainerHealthy status on revision as True
func (rs *RevisionStatus) MarkContainerHealthyTrue() {
	revisionCondSet.Manage(rs).MarkTrue(RevisionConditionContainerHealthy)
}

// MarkContainerHealthyFalse marks ContainerHealthy status on revision as False
func (rs *RevisionStatus) MarkContainerHealthyFalse(reason, message string) {
	revisionCondSet.Manage(rs).MarkFalse(RevisionConditionContainerHealthy, reason, message)
}

// MarkContainerHealthyUnknown marks ContainerHealthy status on revision as Unknown
func (rs *RevisionStatus) MarkContainerHealthyUnknown(reason, message string) {
	revisionCondSet.Manage(rs).MarkUnknown(RevisionConditionContainerHealthy, reason, message)
}

// MarkResourcesAvailableTrue marks ResourcesAvailable status on revision as True
func (rs *RevisionStatus) MarkResourcesAvailableTrue() {
	revisionCondSet.Manage(rs).MarkTrue(RevisionConditionResourcesAvailable)
}

// MarkResourcesAvailableFalse marks ResourcesAvailable status on revision as False
func (rs *RevisionStatus) MarkResourcesAvailableFalse(reason, message string) {
	revisionCondSet.Manage(rs).MarkFalse(RevisionConditionResourcesAvailable, reason, message)
}

// MarkResourcesAvailableUnknown marks ResourcesAvailable status on revision as Unknown
func (rs *RevisionStatus) MarkResourcesAvailableUnknown(reason, message string) {
	revisionCondSet.Manage(rs).MarkUnknown(RevisionConditionResourcesAvailable, reason, message)
}

// PropagateDeploymentStatus takes the Deployment status and applies its values
// to the Revision status.
func (rs *RevisionStatus) PropagateDeploymentStatus(original *appsv1.DeploymentStatus) {
	ds := serving.TransformDeploymentStatus(original)
	cond := ds.GetCondition(serving.DeploymentConditionReady)
	if cond == nil {
		return
	}

	m := revisionCondSet.Manage(rs)
	switch cond.Status {
	case corev1.ConditionTrue:
		m.MarkTrue(RevisionConditionResourcesAvailable)
	case corev1.ConditionFalse:
		m.MarkFalse(RevisionConditionResourcesAvailable, cond.Reason, cond.Message)
	case corev1.ConditionUnknown:
		m.MarkUnknown(RevisionConditionResourcesAvailable, cond.Reason, cond.Message)
	}
}

// PropagateAutoscalerStatus propagates autoscaler's status to the revision's status.
func (rs *RevisionStatus) PropagateAutoscalerStatus(ps *av1alpha1.PodAutoscalerStatus) {
	// Propagate the service name from the PA.
	rs.ServiceName = ps.ServiceName

	// Reflect the PA status in our own.
	cond := ps.GetCondition(av1alpha1.PodAutoscalerConditionReady)
	if cond == nil {
		rs.MarkActiveUnknown("Deploying", "")
		return
	}

	switch cond.Status {
	case corev1.ConditionUnknown:
		rs.MarkActiveUnknown(cond.Reason, cond.Message)
	case corev1.ConditionFalse:
		rs.MarkActiveFalse(cond.Reason, cond.Message)
	case corev1.ConditionTrue:
		rs.MarkActiveTrue()

		// Precondition for PA being active is SKS being active and
		// that entices that |service.endpoints| > 0.
		rs.MarkResourcesAvailableTrue()
		rs.MarkContainerHealthyTrue()
	}
}

// ResourceNotOwnedMessage constructs the status message if ownership on the
// resource is not right.
func ResourceNotOwnedMessage(kind, name string) string {
	return fmt.Sprintf("There is an existing %s %q that we do not own.", kind, name)
}

// ExitCodeReason constructs the status message from an exit code
func ExitCodeReason(exitCode int32) string {
	return fmt.Sprintf("ExitCode%d", exitCode)
}

// RevisionContainerExitingMessage constructs the status message if a container
// fails to come up.
func RevisionContainerExitingMessage(message string) string {
	return fmt.Sprintf("Container failed with: %s", message)
}

// RevisionContainerMissingMessage constructs the status message if a given image
// cannot be pulled correctly.
func RevisionContainerMissingMessage(image string, message string) string {
	return fmt.Sprintf("Unable to fetch image %q: %s", image, message)
}
