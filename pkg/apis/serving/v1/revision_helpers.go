/*
Copyright 2020 The Knative Authors

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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	net "knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
)

const (
	// DefaultUserPort is the system default port value exposed on the user-container.
	DefaultUserPort = 8080

	// UserPortName is the name that will be used for the Port on the
	// Deployment and Pod created by a Revision. This name will be set regardless of if
	// a user specifies a port or the default value is chosen.
	UserPortName = "user-port"

	// QueueAdminPortName specifies the port name for
	// health check and lifecycle hooks for queue-proxy.
	QueueAdminPortName string = "http-queueadm"

	// AutoscalingQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy for autoscaler.
	AutoscalingQueueMetricsPortName = "http-autometric"

	// UserQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy for end user.
	UserQueueMetricsPortName = "http-usermetric"

	AnnotationParseErrorTypeMissing = "Missing"
	AnnotationParseErrorTypeInvalid = "Invalid"
	LabelParserErrorTypeMissing     = "Missing"
	LabelParserErrorTypeInvalid     = "Invalid"
)

type (
	// +k8s:deepcopy-gen=false
	AnnotationParseError struct {
		Type  string
		Value string
		Err   error
	}

	// +k8s:deepcopy-gen=false
	LastPinnedParseError AnnotationParseError
)

func (e LastPinnedParseError) Error() string {
	return fmt.Sprintf("%v lastPinned value: %q", e.Type, e.Value)
}

// GetContainer returns a pointer to the relevant corev1.Container field.
// It is never nil and should be exactly the specified container as guaranteed
// by validation.
func (rs *RevisionSpec) GetContainer() *corev1.Container {
	if len(rs.Containers) > 0 {
		return &rs.Containers[0]
	}
	// Should be unreachable post-validation, but here to ease testing.
	return &corev1.Container{}
}

// IsReachable returns whether or not the revision can be reached by a route.
func (r *Revision) IsReachable() bool {
	return r.ObjectMeta.Labels[serving.RouteLabelKey] != ""
}

// GetProtocol returns the app level network protocol.
func (r *Revision) GetProtocol() (p net.ProtocolType) {
	p = net.ProtocolHTTP1

	ports := r.Spec.GetContainer().Ports
	if len(ports) <= 0 {
		return
	}

	if ports[0].Name == string(net.ProtocolH2C) {
		p = net.ProtocolH2C
	}

	return
}

// SetLastPinned sets the revision's last pinned annotations
// to be the specified time
func (r *Revision) SetLastPinned(t time.Time) {
	if r.ObjectMeta.Annotations == nil {
		r.ObjectMeta.Annotations = make(map[string]string)
	}

	r.ObjectMeta.Annotations[serving.RevisionLastPinnedAnnotationKey] = RevisionLastPinnedString(t)
}

// GetLastPinned returns the time the revision was last pinned
func (r *Revision) GetLastPinned() (time.Time, error) {
	if r.Annotations == nil {
		return time.Time{}, LastPinnedParseError{
			Type: AnnotationParseErrorTypeMissing,
		}
	}

	str, ok := r.ObjectMeta.Annotations[serving.RevisionLastPinnedAnnotationKey]
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

// IsActivationRequired returns true if activation is required.
func (rs *RevisionStatus) IsActivationRequired() bool {
	if c := revisionCondSet.Manage(rs).GetCondition(RevisionConditionActive); c != nil {
		return c.Status != corev1.ConditionTrue
	}
	return false
}

// RevisionLastPinnedString returns a string representation of the specified time
func RevisionLastPinnedString(t time.Time) string {
	return fmt.Sprintf("%d", t.Unix())
}

func (rs *RevisionStatus) duck() *duckv1.Status {
	return &rs.Status
}
