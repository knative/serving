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

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodAutoscaler is a Knative abstraction that encapsulates the interface by which Knative
// components instantiate autoscalers.  This definition is an abstraction that may be backed
// by multiple definitions.  For more information, see the Knative Pluggability presentation:
// https://docs.google.com/presentation/d/10KWynvAJYuOEWy69VBa6bHJVCqIsz1TNdEKosNvcpPY/edit
type PodAutoscaler struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the PodAutoscaler (from the client).
	// +optional
	Spec PodAutoscalerSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the PodAutoscaler (from the controller).
	// +optional
	Status PodAutoscalerStatus `json:"status,omitempty"`
}

// Check that PodAutoscaler can be validated, can be defaulted, and has immutable fields.
var _ apis.Validatable = (*PodAutoscaler)(nil)
var _ apis.Defaultable = (*PodAutoscaler)(nil)
var _ apis.Immutable = (*PodAutoscaler)(nil)

// PodAutoscalerSpec holds the desired state of the PodAutoscaler (from the client).
type PodAutoscalerSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// ServingState holds a value describing the desired state the Kubernetes
	// resources should be in for this PodAutoscaler.
	// TODO(josephburnett): Remove this when the metrics pipeline is sufficient.
	// +optional
	ServingState servingv1alpha1.RevisionServingStateType `json:"servingState,omitempty"`

	// ConcurrencyModel specifies the desired concurrency model
	// (Single or Multi) for the scale target. Defaults to Multi.
	// +optional
	ConcurrencyModel servingv1alpha1.RevisionRequestConcurrencyModelType `json:"concurrencyModel,omitempty"`

	// ScaleTargetRef defines the /scale-able resource that this PodAutoscaler
	// is responsible for quickly right-sizing.
	ScaleTargetRef autoscalingv1.CrossVersionObjectReference `json:"scaleTargetRef"`

	// ServiceName holds the name of a core Kubernetes Service resource that
	// load balances over the pods referenced by the ScaleTargetRef.
	ServiceName string `json:"serviceName"`
}

// PodAutoscalerConditionType is used to communicate the status of the reconciliation process.
type PodAutoscalerConditionType string

const (
	// PodAutoscalerConditionReady is set when the revision is starting to materialize
	// runtime resources, and becomes true when those resources are ready.
	PodAutoscalerConditionReady PodAutoscalerConditionType = "Ready"
	// PodAutoscalerConditionActive is set when the PodAutoscaler's ScaleTargetRef is receiving traffic.
	PodAutoscalerConditionActive PodAutoscalerConditionType = "Active"
)

// PodAutoscalerCondition defines a readiness condition for a PodAutoscaler.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type PodAutoscalerCondition struct {
	Type PodAutoscalerConditionType `json:"type" description:"type of PodAutoscaler condition"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	// We use VolatileTime in place of metav1.Time to exclude this from creating equality.Semantic
	// differences (all other things held constant).
	LastTransitionTime apis.VolatileTime `json:"lastTransitionTime,omitempty" description:"last time the condition transit from one status to another"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// PodAutoscalerStatus communicates the observed state of the PodAutoscaler (from the controller).
type PodAutoscalerStatus struct {
	// Conditions communicates information about ongoing/complete
	// reconciliation processes that bring the "spec" inline with the observed
	// state of the world.
	// +optional
	Conditions []PodAutoscalerCondition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodAutoscalerList is a list of PodAutoscaler resources
type PodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PodAutoscaler `json:"items"`
}

func (r *PodAutoscaler) GetGeneration() int64 {
	return r.Spec.Generation
}

func (r *PodAutoscaler) SetGeneration(generation int64) {
	r.Spec.Generation = generation
}

func (r *PodAutoscaler) GetSpecJSON() ([]byte, error) {
	return json.Marshal(r.Spec)
}

// IsReady looks at the conditions and if the Status has a condition
// PodAutoscalerConditionReady returns true if ConditionStatus is True
func (rs *PodAutoscalerStatus) IsReady() bool {
	if c := rs.GetCondition(PodAutoscalerConditionReady); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

func (rs *PodAutoscalerStatus) GetCondition(t PodAutoscalerConditionType) *PodAutoscalerCondition {
	for _, cond := range rs.Conditions {
		if cond.Type == t {
			return &cond
		}
	}
	return nil
}

func (rs *PodAutoscalerStatus) setCondition(new *PodAutoscalerCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []PodAutoscalerCondition
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
	new.LastTransitionTime = apis.VolatileTime{metav1.NewTime(time.Now())}
	conditions = append(conditions, *new)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	rs.Conditions = conditions
}

func (rs *PodAutoscalerStatus) InitializeConditions() {
	for _, cond := range []PodAutoscalerConditionType{
		PodAutoscalerConditionActive,
		PodAutoscalerConditionReady,
	} {
		if rc := rs.GetCondition(cond); rc == nil {
			rs.setCondition(&PodAutoscalerCondition{
				Type:   cond,
				Status: corev1.ConditionUnknown,
			})
		}
	}
}

func (rs *PodAutoscalerStatus) MarkActive() {
	rs.setCondition(&PodAutoscalerCondition{
		Type:   PodAutoscalerConditionActive,
		Status: corev1.ConditionTrue,
	})
	rs.checkAndMarkReady()
}

func (rs *PodAutoscalerStatus) MarkActivating(reason, message string) {
	for _, cond := range []PodAutoscalerConditionType{
		PodAutoscalerConditionActive,
		PodAutoscalerConditionReady,
	} {
		rs.setCondition(&PodAutoscalerCondition{
			Type:    cond,
			Status:  corev1.ConditionUnknown,
			Reason:  reason,
			Message: message,
		})
	}
}

func (rs *PodAutoscalerStatus) MarkInactive(reason, message string) {
	for _, cond := range []PodAutoscalerConditionType{
		PodAutoscalerConditionActive,
		PodAutoscalerConditionReady,
	} {
		rs.setCondition(&PodAutoscalerCondition{
			Type:    cond,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	}
}

func (rs *PodAutoscalerStatus) checkAndMarkReady() {
	for _, cond := range []PodAutoscalerConditionType{
		PodAutoscalerConditionActive,
	} {
		c := rs.GetCondition(cond)
		if c == nil || c.Status != corev1.ConditionTrue {
			return
		}
	}
	rs.markReady()
}

func (rs *PodAutoscalerStatus) markReady() {
	rs.setCondition(&PodAutoscalerCondition{
		Type:   PodAutoscalerConditionReady,
		Status: corev1.ConditionTrue,
	})
}
