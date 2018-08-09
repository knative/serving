/*
Copyright 2018 The Knative Authors.

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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Route is responsible for configuring ingress over a collection of Revisions.
// Some of the Revisions a Route distributes traffic over may be specified by
// referencing the Configuration responsible for creating them; in these cases
// the Route is additionally responsible for monitoring the Configuration for
// "latest ready" revision changes, and smoothly rolling out latest revisions.
// See also: https://github.com/knative/serving/blob/master/docs/spec/overview.md#route
type Route struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Route (from the client).
	// +optional
	Spec RouteSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Route (from the controller).
	// +optional
	Status RouteStatus `json:"status,omitempty"`
}

// Check that Route may be validated and defaulted.
var _ apis.Validatable = (*Route)(nil)
var _ apis.Defaultable = (*Route)(nil)

// TrafficTarget holds a single entry of the routing table for a Route.
type TrafficTarget struct {
	// Name is optionally used to expose a dedicated hostname for referencing this
	// target exclusively. It has the form: {name}.${route.status.domain}
	// +optional
	Name string `json:"name,omitempty"`

	// RevisionName of a specific revision to which to send this portion of traffic.
	// This is mutually exclusive with ConfigurationName.
	// +optional
	RevisionName string `json:"revisionName,omitempty"`

	// ConfigurationName of a configuration to whose latest revision we will send
	// this portion of traffic. When the "status.latestReadyRevisionName" of the
	// referenced configuration changes, we will automatically migrate traffic
	// from the prior "latest ready" revision to the new one.
	// This field is never set in Route's status, only its spec.
	// This is mutually exclusive with RevisionName.
	// +optional
	ConfigurationName string `json:"configurationName,omitempty"`

	// Percent specifies percent of the traffic to this Revision or Configuration.
	// This defaults to zero if unspecified.
	Percent int `json:"percent"`
}

// RouteSpec holds the desired state of the Route (from the client).
type RouteSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// Traffic specifies how to distribute traffic over a collection of Knative Serving Revisions and Configurations.
	// +optional
	Traffic []TrafficTarget `json:"traffic,omitempty"`
}

// RouteCondition defines a readiness condition.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type RouteCondition struct {
	Type RouteConditionType `json:"type"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	// We use VolatileTime in place of metav1.Time to exclude this from creating equality.Semantic
	// differences (all other things held constant).
	LastTransitionTime VolatileTime `json:"lastTransitionTime,omitempty" description:"last time the condition transit from one status to another"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// RouteConditionType is used to communicate the status of the reconciliation process.
// See also: https://github.com/knative/serving/blob/master/docs/spec/errors.md#error-conditions-and-reporting
type RouteConditionType string

const (
	// RouteConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	RouteConditionReady RouteConditionType = "Ready"

	// RouteConditionAllTrafficAssigned is set to False when the
	// service is not configured properly or has no available
	// backends ready to receive traffic.
	RouteConditionAllTrafficAssigned RouteConditionType = "AllTrafficAssigned"
)

// RouteStatus communicates the observed state of the Route (from the controller).
type RouteStatus struct {
	// Domain holds the top-level domain that will distribute traffic over the provided targets.
	// It generally has the form {route-name}.{route-namespace}.{cluster-level-suffix}
	// +optional
	Domain string `json:"domain,omitempty"`

	// DomainInternal holds the top-level domain that will distribute traffic over the provided
	// targets from inside the cluster. It generally has the form
	// {route-name}.{route-namespace}.svc.cluster.local
	// +optional
	DomainInternal string `json:"domainInternal,omitempty"`

	// Traffic holds the configured traffic distribution.
	// These entries will always contain RevisionName references.
	// When ConfigurationName appears in the spec, this will hold the
	// LatestReadyRevisionName that we last observed.
	// +optional
	Traffic []TrafficTarget `json:"traffic,omitempty"`

	// Conditions communicates information about ongoing/complete
	// reconciliation processes that bring the "spec" inline with the observed
	// state of the world.
	// +optional
	Conditions []RouteCondition `json:"conditions,omitempty"`

	// ObservedGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec and create the Revision.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RouteList is a list of Route resources
type RouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Route `json:"items"`
}

func (r *Route) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *Route) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *Route) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

func (rs *RouteStatus) IsReady() bool {
	if c := rs.GetCondition(RouteConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

func (rs *RouteStatus) GetCondition(t RouteConditionType) *RouteCondition {
	for _, cond := range rs.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (rs *RouteStatus) setCondition(new *RouteCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []RouteCondition
	for _, cond := range rs.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		} else {
			// If we'd only update the LastTransitionTime, then return.
			new.LastTransitionTime = cond.LastTransitionTime
			if reflect.DeepEqual(new, &cond) {
				return
			}
		}
	}
	new.LastTransitionTime = VolatileTime{metav1.NewTime(time.Now())}
	conditions = append(conditions, *new)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	rs.Conditions = conditions
}

func (rs *RouteStatus) InitializeConditions() {
	for _, cond := range []RouteConditionType{
		RouteConditionAllTrafficAssigned,
		RouteConditionReady,
	} {
		if rc := rs.GetCondition(cond); rc == nil {
			rs.setCondition(&RouteCondition{
				Type:   cond,
				Status: corev1.ConditionUnknown,
			})
		}
	}
}

func (rs *RouteStatus) MarkTrafficAssigned() {
	rs.setCondition(&RouteCondition{
		Type:   RouteConditionAllTrafficAssigned,
		Status: corev1.ConditionTrue,
	})
	rs.checkAndMarkReady()
}

func (rs *RouteStatus) markTrafficTargetNotReady(reason, msg string) {
	rs.setCondition(&RouteCondition{
		Type:    RouteConditionAllTrafficAssigned,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: msg,
	})
	// TODO(tcnghia): when we start with new RouteConditionReady every revision,
	// uncomment the short-circuiting below.
	//
	// // Do not downgrade Ready condition.
	// if c := rs.GetCondition(RouteConditionReady); c != nil && c.Status == corev1.ConditionFalse {
	// 	return
	// }
	//
	// For now, the following is harmless because RouteConditionAllTrafficAssigned
	// is the only condition RouteConditionReady depends on.
	rs.setCondition(&RouteCondition{
		Type:    RouteConditionReady,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: msg,
	})
}

func (rs *RouteStatus) markTrafficTargetFailed(reason, msg string) {
	for _, cond := range []RouteConditionType{
		RouteConditionAllTrafficAssigned,
		RouteConditionReady,
	} {
		rs.setCondition(&RouteCondition{
			Type:    cond,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: msg,
		})
	}
}

func (rs *RouteStatus) MarkUnknownTrafficError(msg string) {
	rs.markTrafficTargetNotReady("Unknown", msg)
}

func (rs *RouteStatus) MarkConfigurationNotReady(name string) {
	reason := "RevisionMissing"
	msg := fmt.Sprintf("Configuration %q is waiting for a Revision to become ready.", name)
	rs.markTrafficTargetNotReady(reason, msg)
}

func (rs *RouteStatus) MarkConfigurationFailed(name string) {
	reason := "RevisionMissing"
	msg := fmt.Sprintf("Configuration %q does not have any ready Revision.", name)
	rs.markTrafficTargetFailed(reason, msg)
}

func (rs *RouteStatus) MarkRevisionNotReady(name string) {
	reason := "RevisionMissing"
	msg := fmt.Sprintf("Revision %q is not yet ready.", name)
	rs.markTrafficTargetNotReady(reason, msg)
}

func (rs *RouteStatus) MarkRevisionFailed(name string) {
	reason := "RevisionMissing"
	msg := fmt.Sprintf("Revision %q failed to become ready.", name)
	rs.markTrafficTargetFailed(reason, msg)
}

func (rs *RouteStatus) MarkMissingTrafficTarget(kind, name string) {
	reason := kind + "Missing"
	msg := fmt.Sprintf("%s %q referenced in traffic not found.", kind, name)
	rs.markTrafficTargetFailed(reason, msg)
}

func (rs *RouteStatus) checkAndMarkReady() {
	for _, cond := range []RouteConditionType{
		RouteConditionAllTrafficAssigned,
	} {
		ata := rs.GetCondition(cond)
		if ata == nil || ata.Status != corev1.ConditionTrue {
			return
		}
	}
	rs.markReady()
}

func (rs *RouteStatus) markReady() {
	rs.setCondition(&RouteCondition{
		Type:   RouteConditionReady,
		Status: corev1.ConditionTrue,
	})
}
