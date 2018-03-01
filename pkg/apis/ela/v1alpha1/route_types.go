/*
Copyright 2018 Google LLC.

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
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Route
type Route struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteSpec   `json:"spec,omitempty"`
	Status RouteStatus `json:"status,omitempty"`
}

type ServiceType string

const (
	FunctionService   ServiceType = "function"
	HttpServerService ServiceType = "httpserver"
	ContainerService  ServiceType = "container"
)

type TrafficTarget struct {
	// Optional name to expose this as an external target
	// +optional
	Name string `json:"name,omitempty"`

	// One of these is valid...
	// Revision is the name to a specific revision
	// +optional
	Revision string `json:"revision,omitempty"`

	// Configuration is the name to a configuration, rolls out
	// automatically
	// +optional
	Configuration string `json:"configuration,omitempty"`

	// Specifies percent of the traffic to this Revision or Configuration
	Percent int `json:"percent"`
}

// RouteSpec defines the desired state of Route
type RouteSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64 `json:"generation,omitempty"`

	DomainSuffix string `json:"domainSuffix"`
	// What type of a Service is this
	//	ServiceType ServiceType `json:"serviceType"`
	ServiceType string `json:"serviceType"`

	// Specifies the traffic split between Revisions and Configurations
	Traffic []TrafficTarget `json:"traffic,omitempty"`
}

// RouteCondition defines a readiness condition.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type RouteCondition struct {
	Type RouteConditionType `json:"state"`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// RouteConditionType represents an Route condition value
type RouteConditionType string

const (
	// RouteConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	RouteConditionReady RouteConditionType = "Ready"
	// RouteConditionFailed is set when the service is not configured
	// properly or has no available backends ready to receive traffic.
	RouteConditionFailed RouteConditionType = "Failed"
)

// RouteStatus defines the observed state of Route
type RouteStatus struct {
	Domain string `json:"domain,omitempty"`

	// Specifies the traffic split between Revisions and Configurations
	Traffic []TrafficTarget `json:"traffic,omitempty"`

	Conditions []RouteCondition `json:"conditions,omitempty"`

	// ObservedGeneration is the 'Generation' of the Configuration that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec and create the Revision.
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

func (ess *RouteStatus) SetCondition(new *RouteCondition) {
	if new == nil {
		return
	}

	t := new.Type
	var conditions []RouteCondition
	for _, cond := range ess.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	conditions = append(conditions, *new)
	ess.Conditions = conditions
}

func (ess *RouteStatus) RemoveCondition(t RouteConditionType) {
	var conditions []RouteCondition
	for _, cond := range ess.Conditions {
		if cond.Type != t {
			conditions = append(conditions, cond)
		}
	}
	ess.Conditions = conditions
}
