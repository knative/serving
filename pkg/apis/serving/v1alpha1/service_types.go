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
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Service acts as a top-level container that manages a set of Routes and
// Configurations which implement a network service. Service exists to provide a
// singular abstraction which can be access controlled, reasoned about, and
// which encapsulates software lifecycle decisions such as rollout policy and
// team resource ownership. Service acts only as an orchestrator of the
// underlying Routes and Configurations (much as a kubernetes Deployment
// orchestrates ReplicaSets), and its usage is optional but recommended.
//
// The Service's controller will track the statuses of its owned Configuration
// and Route, reflecting their statuses and conditions as its own.
//
// See also: https://github.com/knative/serving/blob/master/docs/spec/overview.md#service
type Service struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec ServiceSpec `json:"spec,omitempty"`
	// +optional
	Status ServiceStatus `json:"status,omitempty"`
}

// Check that Service may be validated and defaulted.
var _ apis.Validatable = (*Service)(nil)
var _ apis.Defaultable = (*Service)(nil)

// ServiceSpec represents the configuration for the Service object. Exactly one
// of its members (other than Generation) must be specified. Services can either
// track the latest ready revision of a configuration or be pinned to a specific
// revision.
type ServiceSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// RunLatest defines a simple Service. It will automatically
	// configure a route that keeps the latest ready revision
	// from the supplied configuration running.
	// +optional
	RunLatest *RunLatestType `json:"runLatest,omitempty"`

	// Pins this service to a specific revision name. The revision must
	// be owned by the configuration provided.
	// +optional
	Pinned *PinnedType `json:"pinned,omitempty"`
}

type RunLatestType struct {
	// The configuration for this service.
	// +optional
	Configuration ConfigurationSpec `json:"configuration,omitempty"`
}

type PinnedType struct {
	// The revision name to pin this service to until changed
	// to a different service type.
	// +optional
	RevisionName string `json:"revisionName,omitempty"`

	// The configuration for this service.
	// +optional
	Configuration ConfigurationSpec `json:"configuration,omitempty"`
}

type ServiceCondition struct {
	Type ServiceConditionType `json:"type"`

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

// ServiceConditionType represents an Service condition value
type ServiceConditionType string

const (
	// ServiceConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	ServiceConditionReady ServiceConditionType = "Ready"
	// ServiceConditionRoutesReady is set when the service's underlying
	// routes have reported readiness.
	ServiceConditionRoutesReady ServiceConditionType = "RoutesReady"
	// ServiceConditionConfigurationsReady is set when the service's underlying
	// configurations have reported readiness.
	ServiceConditionConfigurationsReady ServiceConditionType = "ConfigurationsReady"
)

type ServiceStatus struct {
	// +optional
	Conditions []ServiceCondition `json:"conditions,omitempty"`

	// From RouteStatus.
	// Domain holds the top-level domain that will distribute traffic over the provided targets.
	// It generally has the form {route-name}.{route-namespace}.{cluster-level-suffix}
	// +optional
	Domain string `json:"domain,omitempty"`

	// From RouteStatus.
	// DomainInternal holds the top-level domain that will distribute traffic over the provided
	// targets from inside the cluster. It generally has the form
	// {route-name}.{route-namespace}.svc.cluster.local
	// +optional
	DomainInternal string `json:"domainInternal,omitempty"`

	// From RouteStatus.
	// Traffic holds the configured traffic distribution.
	// These entries will always contain RevisionName references.
	// When ConfigurationName appears in the spec, this will hold the
	// LatestReadyRevisionName that we last observed.
	// +optional
	Traffic []TrafficTarget `json:"traffic,omitempty"`

	// From ConfigurationStatus.
	// LatestReadyRevisionName holds the name of the latest Revision stamped out
	// from this Service's Configuration that has had its "Ready" condition become "True".
	// +optional
	LatestReadyRevisionName string `json:"latestReadyRevisionName,omitempty"`

	// From ConfigurationStatus.
	// LatestCreatedRevisionName is the last revision that was created from this Service's
	// Configuration. It might not be ready yet, for that use LatestReadyRevisionName.
	// +optional
	LatestCreatedRevisionName string `json:"latestCreatedRevisionName,omitempty"`

	// ObservedGeneration is the 'Generation' of the Service that
	// was last processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceList is a list of Service resources
type ServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Service `json:"items"`
}

func (s *Service) GetGeneration() int64 {
	return s.Spec.Generation
}

func (s *Service) SetGeneration(generation int64) {
	s.Spec.Generation = generation
}

func (s *Service) GetSpecJSON() ([]byte, error) {
	return json.Marshal(s.Spec)
}

func (ss *ServiceStatus) IsReady() bool {
	if c := ss.GetCondition(ServiceConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

func (ss *ServiceStatus) GetCondition(t ServiceConditionType) *ServiceCondition {
	for _, cond := range ss.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (ss *ServiceStatus) setCondition(new *ServiceCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []ServiceCondition
	for _, cond := range ss.Conditions {
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
	ss.Conditions = conditions
}

func (ss *ServiceStatus) InitializeConditions() {
	for _, cond := range []ServiceConditionType{
		ServiceConditionReady,
		ServiceConditionConfigurationsReady,
		ServiceConditionRoutesReady,
	} {
		if rc := ss.GetCondition(cond); rc == nil {
			ss.setCondition(&ServiceCondition{
				Type:   cond,
				Status: corev1.ConditionUnknown,
			})
		}
	}
}

func (ss *ServiceStatus) PropagateConfigurationStatus(cs ConfigurationStatus) {
	ss.LatestReadyRevisionName = cs.LatestReadyRevisionName
	ss.LatestCreatedRevisionName = cs.LatestCreatedRevisionName

	cc := cs.GetCondition(ConfigurationConditionReady)
	if cc == nil {
		return
	}
	sct := []ServiceConditionType{ServiceConditionConfigurationsReady}
	// If the underlying Configuration reported not ready, then bubble it up.
	if cc.Status != corev1.ConditionTrue {
		sct = append(sct, ServiceConditionReady)
	}
	for _, cond := range sct {
		ss.setCondition(&ServiceCondition{
			Type:    cond,
			Status:  cc.Status,
			Reason:  cc.Reason,
			Message: cc.Message,
		})
	}
	if cc.Status == corev1.ConditionTrue {
		ss.checkAndMarkReady()
	}
}

func (ss *ServiceStatus) PropagateRouteStatus(rs RouteStatus) {
	ss.Domain = rs.Domain
	ss.DomainInternal = rs.DomainInternal
	ss.Traffic = rs.Traffic

	rc := rs.GetCondition(RouteConditionReady)
	if rc == nil {
		return
	}
	sct := []ServiceConditionType{ServiceConditionRoutesReady}
	// If the underlying Route reported not ready, then bubble it up.
	if rc.Status != corev1.ConditionTrue {
		sct = append(sct, ServiceConditionReady)
	}
	for _, cond := range sct {
		ss.setCondition(&ServiceCondition{
			Type:    cond,
			Status:  rc.Status,
			Reason:  rc.Reason,
			Message: rc.Message,
		})
	}
	if rc.Status == corev1.ConditionTrue {
		ss.checkAndMarkReady()
	}
}

func (ss *ServiceStatus) checkAndMarkReady() {
	for _, cond := range []ServiceConditionType{
		ServiceConditionConfigurationsReady,
		ServiceConditionRoutesReady,
	} {
		c := ss.GetCondition(cond)
		if c == nil || c.Status != corev1.ConditionTrue {
			return
		}
	}
	ss.markReady()
}

func (ss *ServiceStatus) markReady() {
	ss.setCondition(&ServiceCondition{
		Type:   ServiceConditionReady,
		Status: corev1.ConditionTrue,
	})
}
