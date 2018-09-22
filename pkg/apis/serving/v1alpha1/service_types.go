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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/kmeta"
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

// Check that we can create OwnerReferences to a Service.
var _ kmeta.OwnerRefable = (*Service)(nil)

// Check that ServiceStatus may have its conditions managed.
var _ duckv1alpha1.ConditionsAccessor = (*ServiceStatus)(nil)

// Check that Service implements the Conditions duck type.
var _ = duck.VerifyType(&Service{}, &duckv1alpha1.Conditions{})

// Check that Route implements the [Legacy]Targetable duck type.
var _ = duck.VerifyType(&Service{}, &duckv1alpha1.LegacyTargetable{})
var _ = duck.VerifyType(&Service{}, &duckv1alpha1.Targetable{})

// Check that Service implements the Generation duck type.
var emptyGenService duckv1alpha1.Generation
var _ = duck.VerifyType(&Service{}, &emptyGenService)

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

// ConditionType represents a Service condition value
const (
	// ServiceConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	ServiceConditionReady = duckv1alpha1.ConditionReady
	// ServiceConditionRoutesReady is set when the service's underlying
	// routes have reported readiness.
	ServiceConditionRoutesReady duckv1alpha1.ConditionType = "RoutesReady"
	// ServiceConditionConfigurationsReady is set when the service's underlying
	// configurations have reported readiness.
	ServiceConditionConfigurationsReady duckv1alpha1.ConditionType = "ConfigurationsReady"
)

var serviceCondSet = duckv1alpha1.NewLivingConditionSet(ServiceConditionConfigurationsReady, ServiceConditionRoutesReady)

type ServiceStatus struct {
	// +optional
	Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`

	// From RouteStatus.
	// Domain holds the top-level domain that will distribute traffic over the provided targets.
	// It generally has the form {route-name}.{route-namespace}.{cluster-level-suffix}
	// +optional
	Domain string `json:"domain,omitempty"`

	// From RouteStatus.
	// DomainInternal holds the top-level domain that will distribute traffic over the provided
	// targets from inside the cluster. It generally has the form
	// {route-name}.{route-namespace}.svc.cluster.local
	// DEPRECATED: Use Targetable instead.
	// +optional
	DomainInternal string `json:"domainInternal,omitempty"`

	// Targetable holds the information needed for a Route to be the target of an event.
	// +optional
	Targetable *duckv1alpha1.Targetable `json:"targetable,omitempty"`

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

func (s *Service) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Service")
}

func (ss *ServiceStatus) IsReady() bool {
	return serviceCondSet.Manage(ss).IsHappy()
}

func (ss *ServiceStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return serviceCondSet.Manage(ss).GetCondition(t)
}

func (ss *ServiceStatus) InitializeConditions() {
	serviceCondSet.Manage(ss).InitializeConditions()
}

func (ss *ServiceStatus) PropagateConfigurationStatus(cs ConfigurationStatus) {
	ss.LatestReadyRevisionName = cs.LatestReadyRevisionName
	ss.LatestCreatedRevisionName = cs.LatestCreatedRevisionName

	cc := cs.GetCondition(ConfigurationConditionReady)
	if cc == nil {
		return
	}
	switch {
	case cc.Status == corev1.ConditionUnknown:
		serviceCondSet.Manage(ss).MarkUnknown(ServiceConditionConfigurationsReady, cc.Reason, cc.Message)
	case cc.Status == corev1.ConditionTrue:
		serviceCondSet.Manage(ss).MarkTrue(ServiceConditionConfigurationsReady)
	case cc.Status == corev1.ConditionFalse:
		serviceCondSet.Manage(ss).MarkFalse(ServiceConditionConfigurationsReady, cc.Reason, cc.Message)
	}
}

func (ss *ServiceStatus) PropagateRouteStatus(rs RouteStatus) {
	ss.Domain = rs.Domain
	ss.DomainInternal = rs.DomainInternal
	ss.Targetable = rs.Targetable
	ss.Traffic = rs.Traffic

	rc := rs.GetCondition(RouteConditionReady)
	if rc == nil {
		return
	}
	switch {
	case rc.Status == corev1.ConditionUnknown:
		serviceCondSet.Manage(ss).MarkUnknown(ServiceConditionRoutesReady, rc.Reason, rc.Message)
	case rc.Status == corev1.ConditionTrue:
		serviceCondSet.Manage(ss).MarkTrue(ServiceConditionRoutesReady)
	case rc.Status == corev1.ConditionFalse:
		serviceCondSet.Manage(ss).MarkFalse(ServiceConditionRoutesReady, rc.Reason, rc.Message)
	}
}

// GetConditions returns the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (ss *ServiceStatus) GetConditions() duckv1alpha1.Conditions {
	return ss.Conditions
}

// SetConditions sets the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (ss *ServiceStatus) SetConditions(conditions duckv1alpha1.Conditions) {
	ss.Conditions = conditions
}
