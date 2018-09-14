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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/pkg/apis"
	sapis "github.com/knative/serving/pkg/apis"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Revision is an immutable snapshot of code and configuration.  A revision
// references a container image, and optionally a build that is responsible for
// materializing that container image from source. Revisions are created by
// updates to a Configuration.
//
// See also: https://github.com/knative/serving/blob/master/docs/spec/overview.md#revision
type Revision struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Revision (from the client).
	// +optional
	Spec RevisionSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Revision (from the controller).
	// +optional
	Status RevisionStatus `json:"status,omitempty"`
}

// Check that Revision can be validated, can be defaulted, and has immutable fields.
var _ apis.Validatable = (*Revision)(nil)
var _ apis.Defaultable = (*Revision)(nil)
var _ apis.Immutable = (*Revision)(nil)

// Check that RevisionStatus may have it's conditions managed.
var _ sapis.Conditions = (*RevisionStatus)(nil)

// RevisionTemplateSpec describes the data a revision should have when created from a template.
// Based on: https://github.com/kubernetes/api/blob/e771f807/core/v1/types.go#L3179-L3190
type RevisionTemplateSpec struct {
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec RevisionSpec `json:"spec,omitempty"`
}

// RevisionServingStateType is an enumeration of the levels of serving readiness of the Revision.
// See also: https://github.com/knative/serving/blob/master/docs/spec/errors.md#error-conditions-and-reporting
type RevisionServingStateType string

const (
	// The revision is ready to serve traffic. It should have Kubernetes
	// resources, and the Istio route should be pointed to the given resources.
	RevisionServingStateActive RevisionServingStateType = "Active"
	// The revision is not currently serving traffic, but could be made to serve
	// traffic quickly. It should have Kubernetes resources, but the Istio route
	// should be pointed to the activator.
	RevisionServingStateReserve RevisionServingStateType = "Reserve"
	// The revision has been decommissioned and is not needed to serve traffic
	// anymore. It should not have any Istio routes or Kubernetes resources.
	// A Revision may be brought out of retirement, but it may take longer than
	// it would from a "Reserve" state.
	// Note: currently not set anywhere. See https://github.com/knative/serving/issues/1203
	RevisionServingStateRetired RevisionServingStateType = "Retired"
)

// RevisionRequestConcurrencyModelType is an enumeration of the
// concurrency models supported by a Revision.
// Deprecated in favor of RevisionContainerConcurrencyType.
type RevisionRequestConcurrencyModelType string

const (
	// RevisionRequestConcurrencyModelSingle guarantees that only one
	// request will be handled at a time (concurrently) per instance
	// of Revision Container.
	RevisionRequestConcurrencyModelSingle RevisionRequestConcurrencyModelType = "Single"
	// RevisionRequestConcurencyModelMulti allows more than one request to
	// be handled at a time (concurrently) per instance of Revision
	// Container.
	RevisionRequestConcurrencyModelMulti RevisionRequestConcurrencyModelType = "Multi"
)

// RevisionContainerConcurrencyType is an integer expressing a number of
// in-flight (concurrent) requests.
type RevisionContainerConcurrencyType int64

const (
	// The maximum configurable container concurrency.
	RevisionContainerConcurrencyMax RevisionContainerConcurrencyType = 1000
)

// RevisionSpec holds the desired state of the Revision (from the client).
type RevisionSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// ServingState holds a value describing the desired state the Kubernetes
	// resources should be in for this Revision.
	// Users must not specify this when creating a revision. It is expected
	// that the system will manipulate this based on routability and load.
	// +optional
	ServingState RevisionServingStateType `json:"servingState,omitempty"`

	// ConcurrencyModel specifies the desired concurrency model
	// (Single or Multi) for the
	// Revision. Defaults to Multi.
	// Deprecated in favor of ContainerConcurrency.
	// +optional
	ConcurrencyModel RevisionRequestConcurrencyModelType `json:"concurrencyModel,omitempty"`

	// ContainerConcurrency specifies the maximum allowed
	// in-flight (concurrent) requests per container of the Revision.
	// Defaults to `0` which means unlimited concurrency.
	// This field replaces ConcurrencyModel. A value of `1`
	// is equivalent to `Single` and `0` is equivalent to `Multi`.
	// +optional
	ContainerConcurrency RevisionContainerConcurrencyType `json:"containerConcurrency,omitempty"`

	// ServiceAccountName holds the name of the Kubernetes service account
	// as which the underlying K8s resources should be run. If unspecified
	// this will default to the "default" service account for the namespace
	// in which the Revision exists.
	// This may be used to provide access to private container images by
	// following: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account
	// TODO(ZhiminXiang): verify the corresponding service account exists.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// BuildName optionally holds the name of the Build responsible for
	// producing the container image for its Revision.
	// +optional
	BuildName string `json:"buildName,omitempty"`

	// Container defines the unit of execution for this Revision.
	// In the context of a Revision, we disallow a number of the fields of
	// this Container, including: name, resources, ports, and volumeMounts.
	// TODO(mattmoor): Link to the runtime contract tracked by:
	// https://github.com/knative/serving/issues/627
	// +optional
	Container corev1.Container `json:"container,omitempty"`
}

const (
	// RevisionConditionReady is set when the revision is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	RevisionConditionReady = sapis.ConditionReady
	// RevisionConditionBuildSucceeded is set when the revision has an associated build
	// and is marked True if/once the Build has completed successfully.
	RevisionConditionBuildSucceeded sapis.ConditionType = "BuildSucceeded"
	// RevisionConditionResourcesAvailable is set when underlying
	// Kubernetes resources have been provisioned.
	RevisionConditionResourcesAvailable sapis.ConditionType = "ResourcesAvailable"
	// RevisionConditionContainerHealthy is set when the revision readiness check completes.
	RevisionConditionContainerHealthy sapis.ConditionType = "ContainerHealthy"
	// RevisionConditionActive is set when the revision is receiving traffic.
	RevisionConditionActive sapis.ConditionType = "Active"
)

var revCondSet = sapis.NewOngoingConditionSet(
	RevisionConditionBuildSucceeded, // also tricky, does not get initialized.
	RevisionConditionResourcesAvailable,
	RevisionConditionContainerHealthy,
	RevisionConditionActive) // <-- this one will be tricky. it is a state, not a condition on overall readiness.

func init() {
	revCondSet.SetInits(
		RevisionConditionResourcesAvailable,
		RevisionConditionContainerHealthy,
		RevisionConditionActive)

	revCondSet.SetOverallTrueRequires(
		RevisionConditionContainerHealthy,
		RevisionConditionResourcesAvailable)

	revCondSet.SetOverallUnknownRequires(
		RevisionConditionResourcesAvailable,
		RevisionConditionContainerHealthy,
		RevisionConditionActive)
}

// RevisionStatus communicates the observed state of the Revision (from the controller).
type RevisionStatus struct {
	// ServiceName holds the name of a core Kubernetes Service resource that
	// load balances over the pods backing this Revision. When the Revision
	// is Active, this service would be an appropriate ingress target for
	// targeting the revision.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// Conditions communicates information about ongoing/complete
	// reconciliation processes that bring the "spec" inline with the observed
	// state of the world.
	// +optional
	Conditions []sapis.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec and create the Revision.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LogURL specifies the generated logging url for this particular revision
	// based on the revision url template specified in the controller's config.
	// +optional
	LogURL string `json:"logUrl,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RevisionList is a list of Revision resources
type RevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Revision `json:"items"`
}

func (r *Revision) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *Revision) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *Revision) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

// IsReady looks at the conditions and if the Status has a condition
// RevisionConditionReady returns true if ConditionStatus is True
func (rs *RevisionStatus) IsReady() bool {
	return revCondSet.Using(rs).IsHappy()
}

func (rs *RevisionStatus) IsActivationRequired() bool {
	return !(revCondSet.Using(rs).GetCondition(RevisionConditionActive).IsTrue())
}

func (rs *RevisionStatus) IsRoutable() bool {
	return rs.IsReady() || rs.IsActivationRequired()
}

func (rs *RevisionStatus) GetCondition(t sapis.ConditionType) *sapis.Condition {
	return revCondSet.Using(rs).GetCondition(t)
}

// This is kept for unit test integration.
func (rs *RevisionStatus) setCondition(new *sapis.Condition) {
	if new != nil {
		revCondSet.Using(rs).SetCondition(*new)
	}
}

func (rs *RevisionStatus) InitializeConditions() {
	revCondSet.Using(rs).InitializeConditions()

	// TODO: We don't include BuildSucceeded here because it could confuse users if
	// no `buildName` was specified.
}

func (rs *RevisionStatus) InitializeBuildCondition() {
	revCondSet.Using(rs).InitializeCondition(RevisionConditionBuildSucceeded)
}

func (rs *RevisionStatus) PropagateBuildStatus(bs buildv1alpha1.BuildStatus) {
	bc := bs.GetCondition(buildv1alpha1.BuildSucceeded)
	if bc == nil {
		return
	}
	switch {
	case bc.Status == corev1.ConditionUnknown:
		revCondSet.Using(rs).MarkUnknown(RevisionConditionBuildSucceeded, "Building", bc.Message)
	case bc.Status == corev1.ConditionTrue:
		revCondSet.Using(rs).MarkTrue(RevisionConditionBuildSucceeded)
	case bc.Status == corev1.ConditionFalse:
		revCondSet.Using(rs).MarkFalse(RevisionConditionBuildSucceeded, bc.Reason, bc.Message)
	}
}

func (rs *RevisionStatus) MarkDeploying(reason string) {
	revCondSet.Using(rs).MarkUnknown(RevisionConditionResourcesAvailable, reason, "")
	revCondSet.Using(rs).MarkUnknown(RevisionConditionContainerHealthy, reason, "")
}

func (rs *RevisionStatus) MarkServiceTimeout() {
	revCondSet.Using(rs).MarkFalse(RevisionConditionResourcesAvailable, "ServiceTimeout",
		"Timed out waiting for a service endpoint to become ready")
}

func (rs *RevisionStatus) MarkProgressDeadlineExceeded(message string) {
	revCondSet.Using(rs).MarkFalse(RevisionConditionResourcesAvailable, "ProgressDeadlineExceeded", message)
}

func (rs *RevisionStatus) MarkContainerHealthy() {
	revCondSet.Using(rs).MarkTrue(RevisionConditionContainerHealthy)
}

func (rs *RevisionStatus) MarkResourcesAvailable() {
	revCondSet.Using(rs).MarkTrue(RevisionConditionResourcesAvailable)
}

func (rs *RevisionStatus) MarkActive() {
	revCondSet.Using(rs).MarkTrue(RevisionConditionActive)
}

func (rs *RevisionStatus) MarkActivating(reason, message string) {
	revCondSet.Using(rs).MarkUnknown(RevisionConditionActive, reason, message)
}

func (rs *RevisionStatus) MarkInactive(reason, message string) {
	revCondSet.Using(rs).MarkFalse(RevisionConditionActive, reason, message)
}

func (rs *RevisionStatus) MarkContainerMissing(message string) {
	revCondSet.Using(rs).MarkFalse(RevisionConditionContainerHealthy, "ContainerMissing", message)
}

//
//func (rs *RevisionStatus) markTrue(t sapis.ConditionType) {
//	rs.setCondition(&sapis.Condition{
//		Type:   t,
//		Status: corev1.ConditionTrue,
//	})
//	for _, cond := range []sapis.ConditionType{
//		RevisionConditionContainerHealthy,
//		RevisionConditionResourcesAvailable,
//	} {
//		c := rs.GetCondition(cond)
//		if c == nil || c.Status != corev1.ConditionTrue {
//			return
//		}
//	}
//	rs.setCondition(&sapis.Condition{
//		Type:   RevisionConditionReady,
//		Status: corev1.ConditionTrue,
//	})
//}
//
//func (rs *RevisionStatus) markUnknown(t sapis.ConditionType, reason, message string) {
//	rs.setCondition(&sapis.Condition{
//		Type:    t,
//		Status:  corev1.ConditionUnknown,
//		Reason:  reason,
//		Message: message,
//	})
//	for _, cond := range []sapis.ConditionType{
//		RevisionConditionContainerHealthy,
//		RevisionConditionActive,
//		RevisionConditionResourcesAvailable,
//	} {
//		c := rs.GetCondition(cond)
//		if c == nil || c.Status == corev1.ConditionFalse {
//			// Failed conditions trump unknown conditions
//			return
//		}
//	}
//	rs.setCondition(&sapis.Condition{
//		Type:    RevisionConditionReady,
//		Status:  corev1.ConditionUnknown,
//		Reason:  reason,
//		Message: message,
//	})
//}
//
//func (rs *RevisionStatus) markFalse(t sapis.ConditionType, reason, message string) {
//	for _, cond := range []sapis.ConditionType{
//		t,
//		RevisionConditionReady,
//	} {
//		rs.setCondition(&sapis.Condition{
//			Type:    cond,
//			Status:  corev1.ConditionFalse,
//			Reason:  reason,
//			Message: message,
//		})
//	}
//}

// GetConditions returns the Conditions array. This enables generic handling of
// conditions by implementing the sapis.Conditions interface.
func (rs *RevisionStatus) GetConditions() []sapis.Condition {
	return rs.Conditions
}

// SetConditions sets the Conditions array. This enables generic handling of
// conditions by implementing the sapis.Conditions interface.
func (rs *RevisionStatus) SetConditions(conditions []sapis.Condition) {
	rs.Conditions = conditions
}
