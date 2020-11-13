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

	// ReasonResolvingDigests defines the reason for marking container healthiness status
	// as unknown if the digests for the container images are being resolved.
	ReasonResolvingDigests = "ResolvingDigests"

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

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*Revision) GetConditionSet() apis.ConditionSet {
	return revisionCondSet
}

// GetGroupVersionKind returns the GroupVersionKind.
func (r *Revision) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Revision")
}

// IsReady returns true if the Status condition RevisionConditionReady
// is true and the latest spec has been observed.
func (r *Revision) IsReady() bool {
	rs := r.Status
	return rs.ObservedGeneration == r.Generation &&
		rs.GetCondition(RevisionConditionReady).IsTrue()
}

// IsFailed returns true if the resource has observed the latest generation
// and ready is false.
func (r *Revision) IsFailed() bool {
	rs := r.Status
	return rs.ObservedGeneration == r.Generation &&
		rs.GetCondition(RevisionConditionReady).IsFalse()
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
	// We escape here, because errors sometimes contain `%` and that makes the error message
	// quite poor.
	revisionCondSet.Manage(rs).MarkFalse(RevisionConditionContainerHealthy, reason, "%s", message)
}

// MarkContainerHealthyUnknown marks ContainerHealthy status on revision as Unknown
func (rs *RevisionStatus) MarkContainerHealthyUnknown(reason, message string) {
	// We escape here, because errors sometimes contain `%` and that makes the error message
	// quite poor.
	revisionCondSet.Manage(rs).MarkUnknown(RevisionConditionContainerHealthy, reason, "%s", message)
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

	// Don't mark the resources available, if deployment status already determined
	// it isn't so.
	resUnavailable := rs.GetCondition(RevisionConditionResourcesAvailable).IsFalse()
	if ps.IsScaleTargetInitialized() && !resUnavailable {
		// Precondition for PA being initialized is SKS being active and
		// that implies that |service.endpoints| > 0.
		rs.MarkResourcesAvailableTrue()
		rs.MarkContainerHealthyTrue()
	}

	switch cond.Status {
	case corev1.ConditionUnknown:
		rs.MarkActiveUnknown(cond.Reason, cond.Message)
	case corev1.ConditionFalse:
		// Here we have 2 things coming together at the same time:
		// 1. The ready is False, meaning the revision is scaled to 0
		// 2. Initial scale was never achieved, which means we failed to progress
		//    towards initial scale during the progress deadline period and scaled to 0
		//		failing to activate.
		// So mark the revision as failed at that point.
		// See #8922 for details. When we try to scale to 0, we force the Deployment's
		// Progress status to become `true`, since successful scale down means
		// progress has been achieved.
		// There's the possibility of the revision reconciler reconciling PA before
		// the ServiceName is populated, and therefore even though we will mark
		// ScaleTargetInitialized down the road, we would have marked resources
		// unavailable here, and have no way of recovering later.
		// If the ResourcesAvailable is already false, don't override the message.
		if !ps.IsScaleTargetInitialized() && !resUnavailable && ps.ServiceName != "" {
			rs.MarkResourcesAvailableFalse(ReasonProgressDeadlineExceeded,
				"Initial scale was never achieved")
		}
		rs.MarkActiveFalse(cond.Reason, cond.Message)
	case corev1.ConditionTrue:
		rs.MarkActiveTrue()

		// Precondition for PA being active is SKS being active and
		// that implies that |service.endpoints| > 0.
		//
		// Note: This is needed for backwards compatibility as we're adding the new
		// ScaleTargetInitialized condition to gate readiness.
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
	return fmt.Sprint("ExitCode", exitCode)
}

// RevisionContainerExitingMessage constructs the status message if a container
// fails to come up.
func RevisionContainerExitingMessage(message string) string {
	return fmt.Sprint("Container failed with: ", message)
}

// RevisionContainerMissingMessage constructs the status message if a given image
// cannot be pulled correctly.
func RevisionContainerMissingMessage(image string, message string) string {
	return fmt.Sprintf("Unable to fetch image %q: %s", image, message)
}
