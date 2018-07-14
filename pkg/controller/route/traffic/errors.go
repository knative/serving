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

type unreadyTargetError struct {
	kind string // Kind of the traffic target.
	name string // Name of the target that isn't ready.
}

var _ TargetError = (*unreadyTargetError)(nil)

// Error implements error.
func (e *unreadyTargetError) Error() string {
	return fmt.Sprintf("%s %q referenced in traffic not ready", e.kind, e.name)
}

// MarkBadTrafficTarget implements TargetError.
func (e *unreadyTargetError) MarkBadTrafficTarget(rs *v1alpha1.RouteStatus) {
	rs.MarkUnreadyTarget(e.kind, e.name)
}

type latestRevisionDeletedErr struct {
	name string // name of the Configuration whose LatestReadyResivion is deletd.
}

var _ TargetError = (*latestRevisionDeletedErr)(nil)

// Error implements error.
func (e *latestRevisionDeletedErr) Error() string {
	return fmt.Sprintf("Configuration %q has its LatestReadyResivion deleted", e.name)
}

// MarkBadTrafficTarget implements TargetError.
func (e *latestRevisionDeletedErr) MarkBadTrafficTarget(rs *v1alpha1.RouteStatus) {
	rs.MarkDeletedLatestRevisionTarget(e.name)
}

// errUnreadyTarget returns a TargetError for a target that is not ready.
func errUnreadyTarget(kind string, name string) TargetError {
	return &unreadyTargetError{kind: kind, name: name}
}

// errUnreadyConfiguration returns a TargetError for a Configuration that is not ready.
func errUnreadyConfiguration(name string) TargetError {
	return &unreadyTargetError{kind: "Configuration", name: name}
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

// errNotRoutableRevision returns a TargetError for a Revision that is
// neither Ready nor Inactive.  The spec doesn't have a special case for this
// kind of error, so we are using RevisionMissing here.
func errUnreadyRevision(name string) TargetError {
	return &unreadyTargetError{
		kind: "Revision",
		name: name,
	}
}

// errDeletedRevision returns ad TargetError for Configuration whose latest Revision is deleted.
func errDeletedRevision(configName string) TargetError {
	return &latestRevisionDeletedErr{name: configName}
}

func checkConfiguration(c *v1alpha1.Configuration) TargetError {
	cs := c.Status
	if cs.LatestCreatedRevisionName == "" {
		// Configuration has not any Revision.
		return errUnreadyConfiguration(c.Name)
	}
	if cs.LatestReadyRevisionName == "" {
		cond := cs.GetCondition(v1alpha1.ConfigurationConditionReady)
		// Since LatestCreatedRevisionName is already set, cond isn't nil.
		switch cond.Status {
		case corev1.ConditionUnknown:
			// Configuration was never Ready.
			return errUnreadyConfiguration(c.Name)
		case corev1.ConditionFalse:
			// ConfigurationConditionReady was set before, but the
			// LatestCreatedRevisionName was deleted.
			return errDeletedRevision(c.Name)
		}
	}
	return nil
}
