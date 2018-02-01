/*
Copyright 2017 The Kubernetes Authors.

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
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElaService
type ElaService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElaServiceSpec   `json:"spec,omitempty"`
	Status ElaServiceStatus `json:"status,omitempty"`
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

	// RevisionTemplate is the name to a revisiontemplate, rolls out
	// automatically
	// +optional
	RevisionTemplate string `json:"revisionTemplate,omitempty"`

	// Specifies percent of the traffic to this Revision or RevisionTemplate
	Percent int `json:"percent"`
}

// ElaServiceSpec defines the desired state of ElaService
type ElaServiceSpec struct {
	// TODOD: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	Generation int64 `json:"generation,omitempty"`

	DomainSuffix string `json:"domainSuffix"`
	// What type of a Service is this
	//	ServiceType ServiceType `json:"serviceType"`
	ServiceType string `json:"serviceType"`

	// Specifies the traffic split between Revisions and RevisionTemplates
	Traffic []TrafficTarget `json:"traffic,omitempty"`
}

// ElaServiceCondition defines a readiness condition.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type ElaServiceCondition struct {
	Type ElaServiceConditionType `json:"state"`

	// TODO: Where can we get a proper ConditionStatus?
	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ElaServiceConditionType represents an ElaService condition value
type ElaServiceConditionType string

const (
	// ElaServiceConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	ElaServiceConditionReady ElaServiceConditionType = "Ready"
	// ElaServiceConditionFailed is set when the service is not configured
	// properly or has no available backends ready to receive traffic.
	ElaServiceConditionFailed ElaServiceConditionType = "Failed"
)

// ElaServiceStatus defines the observed state of ElaService
type ElaServiceStatus struct {
	Domain string `json:"domain,omitempty"`

	// Specifies the traffic split between Revisions and RevisionTemplates
	Traffic []TrafficTarget `json:"traffic,omitempty"`

	Conditions []ElaServiceCondition `json:"conditions,omitempty"`

	// ReconciledGeneration is the 'Generation' of the RevisionTemplate that
	// was last processed by the controller. The reconciled generation is updated
	// even if the controller failed to process the spec and create the Revision.
	ReconciledGeneration int64 `json:"reconciledGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElaServiceList is a list of ElaService resources
type ElaServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ElaService `json:"items"`
}
