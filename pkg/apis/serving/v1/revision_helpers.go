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
	"time"

	corev1 "k8s.io/api/core/v1"
	net "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/kmeta"
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
	QueueAdminPortName = "http-queueadm"

	// AutoscalingQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy for autoscaler.
	AutoscalingQueueMetricsPortName = "http-autometric"

	// UserQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy for end user.
	UserQueueMetricsPortName = "http-usermetric"
)

// RoutingState represents states of a revision with regards to serving a route.
type RoutingState string

const (
	// RoutingStateUnset is the empty value for routing state, this state is unexpected.
	RoutingStateUnset RoutingState = ""

	// RoutingStatePending is a state after a revision is created, but before
	// its routing state has been determined. It is treated like active for the purposes
	// of revision garbage collection.
	RoutingStatePending RoutingState = "pending"

	// RoutingStateActive is a state for a revision which is actively referenced by a Route.
	RoutingStateActive RoutingState = "active"

	// RoutingStateReserve is a state for a revision which is no longer referenced by a Route,
	// and is scaled down, but may be rapidly pinned to a route to be made active again.
	RoutingStateReserve RoutingState = "reserve"
)

// GetContainer returns a pointer to the relevant corev1.Container field.
// It is never nil and should be exactly the specified container if len(containers) == 1 or
// if there are multiple containers it returns the container which has Ports
// as guaranteed by validation.
func (rs *RevisionSpec) GetContainer() *corev1.Container {
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
	// Should be unreachable post-validation, but here to ease testing.
	return &corev1.Container{}
}

// SetRoutingState sets the routingState label on this Revision and updates the
// routingStateModified annotation.
func (r *Revision) SetRoutingState(state RoutingState, tm time.Time) {
	stateStr := string(state)
	if t := r.Annotations[serving.RoutingStateModifiedAnnotationKey]; t != "" &&
		r.Labels[serving.RoutingStateLabelKey] == stateStr {
		return // Don't update timestamp if no change.
	}

	r.Labels = kmeta.UnionMaps(r.Labels,
		map[string]string{serving.RoutingStateLabelKey: stateStr})

	r.Annotations = kmeta.UnionMaps(r.Annotations,
		map[string]string{
			serving.RoutingStateModifiedAnnotationKey: RoutingStateModifiedString(tm),
		},
	)
}

// RoutingStateModifiedString gives a formatted now timestamp.
func RoutingStateModifiedString(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// GetRoutingState retrieves the RoutingState label.
func (r *Revision) GetRoutingState() RoutingState {
	return RoutingState(r.Labels[serving.RoutingStateLabelKey])
}

// GetRoutingStateModified retrieves the RoutingStateModified annotation.
func (r *Revision) GetRoutingStateModified() time.Time {
	val := r.Annotations[serving.RoutingStateModifiedAnnotationKey]
	if val == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// IsReachable returns whether or not the revision can be reached by a route.
func (r *Revision) IsReachable() bool {
	return RoutingState(r.Labels[serving.RoutingStateLabelKey]) == RoutingStateActive
}

// GetProtocol returns the app level network protocol.
func (r *Revision) GetProtocol() net.ProtocolType {
	ports := r.Spec.GetContainer().Ports
	if len(ports) > 0 && ports[0].Name == string(net.ProtocolH2C) {
		return net.ProtocolH2C
	}

	return net.ProtocolHTTP1
}

// IsActivationRequired returns true if activation is required.
func (rs *RevisionStatus) IsActivationRequired() bool {
	c := revisionCondSet.Manage(rs).GetCondition(RevisionConditionActive)
	return c != nil && c.Status != corev1.ConditionTrue
}
