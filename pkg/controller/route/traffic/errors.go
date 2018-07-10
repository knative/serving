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

type unreadyConfigTargetError struct {
	name string // Name of the Configuration that has no Ready target.
}

var _ TargetError = (*unreadyConfigTargetError)(nil)

// Error implements error.
func (e *unreadyConfigTargetError) Error() string {
	return fmt.Sprintf("Configuraion %q referenced in traffic not ready", e.name)
}

// MarkBadTrafficTarget implements TargetError.
func (e *unreadyConfigTargetError) MarkBadTrafficTarget(rs *v1alpha1.RouteStatus) {
	rs.MarkUnreadyConfigurationTarget(e.name)
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

// errEmptyConfiguration returns a TargetError for a Configuration that never has a LatestCreatedRevisionName.
func errEmptyConfiguration(configName string) TargetError {
	return &unreadyConfigTargetError{name: configName}
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
func errNotRoutableRevision(name string) TargetError {
	return &missingTargetError{
		kind: "Revision",
		name: name,
	}
}

// errDeletedRevision returns ad TargetError for Configuration whose latest Revision is deleted.
func errDeletedRevision(configName string) TargetError {
	return &latestRevisionDeletedErr{name: configName}
}

func CheckConfigurationErr(c *v1alpha1.Configuration) TargetError {
	cs := c.Status
	if cs.LatestCreatedRevisionName == "" {
		// Configuration has not any Revision.
		return errEmptyConfiguration(c.Name)
	}
	if cs.LatestReadyRevisionName == "" {
		cond := cs.GetCondition(v1alpha1.ConfigurationConditionReady)
		// Since LatestCreatedRevisionName is already set, cond isn't nil.
		switch cond.Status {
		case corev1.ConditionUnknown:
			// Configuration was never Ready.
			return errNotRoutableRevision(cs.LatestCreatedRevisionName)
		case corev1.ConditionFalse:
			// ConfigurationConditionReady was set before, but the
			// LatestCreatedRevisionName was deleted.
			return errDeletedRevision(c.Name)
		}
	}
	return nil
}
