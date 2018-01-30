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

// ActiveDeploymentState specifies the state that the deployment
// can be in. Currently it can be either next or current
// TODO: Use this the below is fixed
// https://github.com/kubernetes-incubator/apiserver-builder/issues/176
//type ActiveDeploymentState string
//
//const (
//	// ActiveDeploymentStateNext specifies that the specified ElaDeployment
//	// is the next one.
//	ActiveDeploymentStateNext ActiveDeploymentState = "next"
//	// ActiveDeploymentStateNext specifies that the specified ElaDeployment
//	// is the current one.
//	ActiveDeploymentStateCurrent ActiveDeploymentState = "current"
//)

// TODO: Use this the below is fixed
// https://github.com/kubernetes-incubator/apiserver-builder/issues/176
//type ServiceType string

//const (
//	Function   ServiceType = "function"
//	HttpServer ServiceType = "httpserver"
//	Container  ServiceType = "container"
//)

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
	RevisionTemplate string `json:"revisionTemplate"`

	// Specifies percent of the traffic to this Revision or RevisionTemplate
	Percent int `json:"percent"`
}

type Rollout struct {
	// List of targets to split the traffic between.
	// Their percent must sum to 100%
	Traffic []TrafficTarget `json:"traffic,omitempty"`
}

// ElaServiceSpec defines the desired state of ElaService
type ElaServiceSpec struct {
	/////////////////////////////////////////////////////////////////////////
	// Latest proposed spec
	/////////////////////////////////////////////////////////////////////////
	DomainSuffix string `json:"domainSuffix"`
	// What type of a Service is this
	//	ServiceType ServiceType `json:"serviceType"`
	ServiceType string `json:"serviceType"`

	// Specifies the traffic split between Revisions and RevisionTemplates
	Rollout Rollout `json:"rollout,omitempty"`
	/////////////////////////////////////////////////////////////////////////
	// End of latest proposed spec
	/////////////////////////////////////////////////////////////////////////

	/////////////////////////////////////////////////////////////////////////
	// ** OLD ** stuff not in the current Ela API yet. Leaving things here
	// while we move to the new design so that things don't completely break.
	/////////////////////////////////////////////////////////////////////////
	Current              string `json:"current"`
	Next                 string `json:"next"`
	RolloutPercentToNext int    `json:"rolloutPercentToNext"`
	ForceReconcile       string `json:"forceReconcile"`
}

// ElaServiceCondition defines a readiness condition for a ElaDeployment.
// See: https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#typical-status-properties
type ElaServiceCondition struct {
	// TODO: Use this the below is fixed
	// https://github.com/kubernetes-incubator/apiserver-builder/issues/176
	// Type ElaDeploymentConditionType `json:"state"`
	Type string `json:"type" description:"type of ElaDeployment condition"`

	// TODO: Where can we get a proper ConditionStatus?
	Status string `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`
	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ElaServiceStatus defines the observed state of ElaService
type ElaServiceStatus struct {
	// Current is the name of current Revision. Controller updates this to next when
	// rolloutPercentage hits 100
	Current string `json:"current"`

	// Current rolloutPercentage. When 100, current=next
	RolloutPercentage string `json:"rolloutPercentage"`

	// Next is the name of the next Revision, rolloutPercentage specifies how
	// much of the traffic is being routed to Next.
	Next string `json:"next"`

	// TODO: Add conditions as they are still quite a bit in flux.
	Conditions []ElaServiceCondition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ElaServiceList is a list of ElaService resources
type ElaServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ElaService `json:"items"`
}
