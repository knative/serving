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

package v1alpha1

import (
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	net "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/apis"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

const (
	// UserPortName is the name that will be used for the Port on the
	// Deployment and Pod created by a Revision. This name will be set regardless of if
	// a user specifies a port or the default value is chosen.
	UserPortName = v1.UserPortName

	// DefaultUserPort is the default port value the QueueProxy will
	// use for connecting to the user container.
	DefaultUserPort = v1.DefaultUserPort

	// QueueAdminPortName specifies the port name for
	// health check and lifecycle hooks for queue-proxy.
	QueueAdminPortName = v1.QueueAdminPortName

	// AutoscalingQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy for autoscaler.
	AutoscalingQueueMetricsPortName = v1.AutoscalingQueueMetricsPortName

	// UserQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy for end user.
	UserQueueMetricsPortName = v1.UserQueueMetricsPortName

	AnnotationParseErrorTypeMissing = v1.AnnotationParseErrorTypeMissing
	AnnotationParseErrorTypeInvalid = v1.AnnotationParseErrorTypeInvalid
	LabelParserErrorTypeMissing     = v1.LabelParserErrorTypeMissing
	LabelParserErrorTypeInvalid     = v1.LabelParserErrorTypeInvalid
)

var revCondSet = apis.NewLivingConditionSet(
	RevisionConditionResourcesAvailable,
	RevisionConditionContainerHealthy,
)

// GetConditionSet retrieves the ConditionSet of the Revision. Implements the KRShaped interface.
func (*Revision) GetConditionSet() apis.ConditionSet {
	return revCondSet
}

func (r *Revision) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Revision")
}

// GetContainer returns a pointer to the relevant corev1.Container field.
// It is never nil and should be exactly the specified container if len(containers) == 1 or
// if there are multiple containers it returns the container which has Ports
// as guaranteed by validation.
func (rs *RevisionSpec) GetContainer() *corev1.Container {
	if rs.DeprecatedContainer != nil {
		return rs.DeprecatedContainer
	}
	switch {
	case len(rs.Containers) == 1:
		return &rs.Containers[0]
	case len(rs.Containers) > 1:
		for i := range rs.Containers {
			if len(rs.Containers[i].Ports) != 0 {
				return &rs.Containers[i]
			}
		}
	}
	// Should be unreachable post-validation, but is here to ease testing.
	return &corev1.Container{}
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

// GetProtocol returns the app level network protocol.
func (r *Revision) GetProtocol() net.ProtocolType {
	ports := r.Spec.GetContainer().Ports
	if len(ports) > 0 && ports[0].Name == string(net.ProtocolH2C) {
		return net.ProtocolH2C
	}

	return net.ProtocolHTTP1
}

// IsReady looks at the conditions and if the Status has a condition
// RevisionConditionReady returns true if ConditionStatus is True
func (rs *RevisionStatus) IsReady() bool {
	return revCondSet.Manage(rs).IsHappy()
}

// IsActivationRequired returns true if activation is required.
func (rs *RevisionStatus) IsActivationRequired() bool {
	if c := revCondSet.Manage(rs).GetCondition(RevisionConditionActive); c != nil {
		return c.Status != corev1.ConditionTrue
	}
	return false
}

func (rs *RevisionStatus) GetCondition(t apis.ConditionType) *apis.Condition {
	return revCondSet.Manage(rs).GetCondition(t)
}

func (rs *RevisionStatus) InitializeConditions() {
	revCondSet.Manage(rs).InitializeConditions()
}

// MarkResourceNotConvertible adds a Warning-severity condition to the resource noting that
// it cannot be converted to a higher version.
func (rs *RevisionStatus) MarkResourceNotConvertible(err *CannotConvertError) {
	revCondSet.Manage(rs).SetCondition(apis.Condition{
		Type:     ConditionTypeConvertible,
		Status:   corev1.ConditionFalse,
		Severity: apis.ConditionSeverityWarning,
		Reason:   err.Field,
		Message:  err.Message,
	})
}

const (
	// NotOwned defines the reason for marking revision availability status as
	// false due to resource ownership issues.
	NotOwned = v1.ReasonNotOwned

	// Deploying defines the reason for marking revision availability status as
	// unknown if the revision is still deploying.
	Deploying = v1.ReasonDeploying

	// ProgressDeadlineExceeded defines the reason for marking revision availability
	// status as false if progress has exceeded the deadline.
	ProgressDeadlineExceeded = v1.ReasonProgressDeadlineExceeded

	// ContainerMissing defines the reason for marking container healthiness status
	// as false if the a container image for the revision is missing.
	ContainerMissing = v1.ReasonContainerMissing
)

// MarkResourcesAvailableTrue marks ResourcesAvailable status on revision as True
func (rs *RevisionStatus) MarkResourcesAvailableTrue() {
	revCondSet.Manage(rs).MarkTrue(RevisionConditionResourcesAvailable)
}

// MarkResourcesAvailableFalse marks ResourcesAvailable status on revision as False
func (rs *RevisionStatus) MarkResourcesAvailableFalse(reason, message string) {
	revCondSet.Manage(rs).MarkFalse(RevisionConditionResourcesAvailable, reason, message)
}

// MarkResourcesAvailableUnknown marks ResourcesAvailable status on revision as Unknown
func (rs *RevisionStatus) MarkResourcesAvailableUnknown(reason, message string) {
	revCondSet.Manage(rs).MarkUnknown(RevisionConditionResourcesAvailable, reason, message)
}

// MarkContainerHealthyTrue marks ContainerHealthy status on revision as True
func (rs *RevisionStatus) MarkContainerHealthyTrue() {
	revCondSet.Manage(rs).MarkTrue(RevisionConditionContainerHealthy)
}

// MarkContainerHealthyFalse marks ContainerHealthy status on revision as False
func (rs *RevisionStatus) MarkContainerHealthyFalse(reason, message string) {
	revCondSet.Manage(rs).MarkFalse(RevisionConditionContainerHealthy, reason, message)
}

// MarkContainerHealthyUnknown marks ContainerHealthy status on revision as Unknown
func (rs *RevisionStatus) MarkContainerHealthyUnknown(reason, message string) {
	revCondSet.Manage(rs).MarkUnknown(RevisionConditionContainerHealthy, reason, message)
}

// MarkActiveTrue marks Active status on revision as True
func (rs *RevisionStatus) MarkActiveTrue() {
	revCondSet.Manage(rs).MarkTrue(RevisionConditionActive)
}

// MarkActiveFalse marks Active status on revision as False
func (rs *RevisionStatus) MarkActiveFalse(reason, message string) {
	revCondSet.Manage(rs).MarkFalse(RevisionConditionActive, reason, message)
}

// MarkActiveUnknown marks Active status on revision as Unknown
func (rs *RevisionStatus) MarkActiveUnknown(reason, message string) {
	revCondSet.Manage(rs).MarkUnknown(RevisionConditionActive, reason, message)
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

// RevisionContainerMissingMessage constructs the status message if a given image
// cannot be pulled correctly.
func RevisionContainerMissingMessage(image, message string) string {
	return fmt.Sprintf("Unable to fetch image %q: %s", image, message)
}

// RevisionContainerExitingMessage constructs the status message if a container
// fails to come up.
func RevisionContainerExitingMessage(message string) string {
	return "Container failed with: " + message
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

// +k8s:deepcopy-gen=false
type AnnotationParseError struct {
	Type  string
	Value string
	Err   error
}

// +k8s:deepcopy-gen=false
type LastPinnedParseError AnnotationParseError

func (e LastPinnedParseError) Error() string {
	return fmt.Sprintf("%v lastPinned value: %q", e.Type, e.Value)
}

func RevisionLastPinnedString(t time.Time) string {
	return fmt.Sprintf("%d", t.Unix())
}

func (r *Revision) SetLastPinned(t time.Time) {
	if r.Annotations == nil {
		r.Annotations = make(map[string]string, 1)
	}

	r.Annotations[serving.RevisionLastPinnedAnnotationKey] = RevisionLastPinnedString(t)
}

func (r *Revision) GetLastPinned() (time.Time, error) {
	if r.Annotations == nil {
		return time.Time{}, LastPinnedParseError{
			Type: AnnotationParseErrorTypeMissing,
		}
	}

	str, ok := r.Annotations[serving.RevisionLastPinnedAnnotationKey]
	if !ok {
		// If a revision is past the create delay without an annotation it is stale
		return time.Time{}, LastPinnedParseError{
			Type: AnnotationParseErrorTypeMissing,
		}
	}

	secs, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return time.Time{}, LastPinnedParseError{
			Type:  AnnotationParseErrorTypeInvalid,
			Value: str,
			Err:   err,
		}
	}

	return time.Unix(secs, 0), nil
}

// PropagateDeploymentStatus takes the Deployment status and applies its values
// to the Revision status.
func (rs *RevisionStatus) PropagateDeploymentStatus(original *appsv1.DeploymentStatus) {
	ds := serving.TransformDeploymentStatus(original)
	cond := ds.GetCondition(serving.DeploymentConditionReady)
	if cond == nil {
		return
	}

	switch cond.Status {
	case corev1.ConditionTrue:
		rs.MarkResourcesAvailableTrue()
	case corev1.ConditionFalse:
		rs.MarkResourcesAvailableFalse(cond.Reason, cond.Message)
	case corev1.ConditionUnknown:
		rs.MarkResourcesAvailableUnknown(cond.Reason, cond.Message)
	}
}
