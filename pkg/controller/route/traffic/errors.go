/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package traffic

import (
	"fmt"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// TargetError gives details about an invalid traffic target.
type TargetError interface {
	error

	// MarkBadTrafficTarget marks a RouteStatus with Condition corresponding
	// to the error case of the traffic target.
	MarkBadTrafficTarget(rs *v1alpha1.RouteStatus)

	// GetConditionStatus returns corev1.ConditionUnknown or corev1.ConditionFalse
	// depending on the kind of error we encounter.
	GetConditionStatus() corev1.ConditionStatus
}

type missingTargetError struct {
	kind string // Kind of the traffic target, e.g. Configuration/Revision.
	name string // Name of the traffic target.
}

var _ TargetError = (*missingTargetError)(nil)

// Error implements error.
func (e *missingTargetError) Error() string {
	return fmt.Sprintf("%v %q referenced in traffic not found", e.kind, e.name)
}

// MarkBadTrafficTarget implements TargetError.
func (e *missingTargetError) MarkBadTrafficTarget(rs *v1alpha1.RouteStatus) {
	rs.MarkMissingTrafficTarget(e.kind, e.name)
}

// GetConditionStatus implements TargetError.
func (e *missingTargetError) GetConditionStatus() corev1.ConditionStatus {
	return corev1.ConditionFalse
}

type unreadyConfigError struct {
	name   string                 // Name of the config that isn't ready.
	status corev1.ConditionStatus // Status of the unready config.
}

var _ TargetError = (*unreadyConfigError)(nil)

// Error implements error.
func (e *unreadyConfigError) Error() string {
	return fmt.Sprintf("Configuration %q referenced in traffic not ready: %v", e.name, e.status)
}

// MarkBadTrafficTarget implements TargetError.
func (e *unreadyConfigError) MarkBadTrafficTarget(rs *v1alpha1.RouteStatus) {
	switch e.status {
	case corev1.ConditionFalse:
		rs.MarkFailedConfig(e.name)
	default:
		rs.MarkUnreadyConfig(e.name)
	}
}

func (e *unreadyConfigError) GetConditionStatus() corev1.ConditionStatus {
	return e.status
}

type unreadyRevisionError struct {
	name   string                 // Name of the config that isn't ready.
	status corev1.ConditionStatus // Status of the unready config.
}

var _ TargetError = (*unreadyRevisionError)(nil)

// Error implements error.
func (e *unreadyRevisionError) Error() string {
	return fmt.Sprintf("Revision %q referenced in traffic not ready: %v", e.name, e.status)
}

// MarkBadTrafficTarget implements TargetError.
func (e *unreadyRevisionError) MarkBadTrafficTarget(rs *v1alpha1.RouteStatus) {
	switch e.status {
	case corev1.ConditionFalse:
		rs.MarkFailedRevision(e.name)
	default:
		rs.MarkUnreadyRevision(e.name)
	}
}

func (e *unreadyRevisionError) GetConditionStatus() corev1.ConditionStatus {
	return e.status
}

// errUnreadyConfiguration returns a TargetError for a Configuration that is not ready.
func errUnreadyConfiguration(config *v1alpha1.Configuration) TargetError {
	status := corev1.ConditionUnknown
	if c := config.Status.GetCondition(v1alpha1.ConfigurationConditionReady); c != nil {
		status = c.Status
	}
	return &unreadyConfigError{
		name:   config.Name,
		status: status,
	}
}

// errUnreadyRevision returns a TargetError for a Revision that is not ready.
func errUnreadyRevision(rev *v1alpha1.Revision) TargetError {
	status := corev1.ConditionUnknown
	if c := rev.Status.GetCondition(v1alpha1.RevisionConditionReady); c != nil {
		status = c.Status
	}
	return &unreadyRevisionError{
		name:   rev.Name,
		status: status,
	}
}

// errMissingConfiguration returns a TargetError for a Configuration what does not exist.
func errMissingConfiguration(name string) TargetError {
	return &missingTargetError{
		kind: "Configuration",
		name: name,
	}
}

// errMissingRevision returns a TargetError for a Revision that does not exist.
func errMissingRevision(name string) TargetError {
	return &missingTargetError{
		kind: "Revision",
		name: name,
	}
}
