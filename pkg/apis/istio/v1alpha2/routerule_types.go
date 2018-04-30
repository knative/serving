/*
Copyright 2018 Google LLC

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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RouteRule
type RouteRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RouteRuleSpec `json:"spec,omitempty"`
}

// Istio route looks like so, but couldn't find a k8s/go definition for it
// so we'll just create one. This is terrible, but it just might work for
// now, but if things change on their end, this will most certainly break :(
//  spec:
//    destination:
//      # this matches what's in the ingress rule as a placeholder k8s service
//      name: k8s-placeholder-service
//    route:
//    - destination:
//        name: revision-service-1
//      match:
//        request:
//          headers:
//            authority:
//              regex: foo.example.com
//      weight: 90
//    - destination:
//        name: revision-service-2
//        namespace: revision-2-namespace
//      weight: 10
//  # https://github.com/istio/istio/blob/master/tests/helm/templates/rule-default-route-append-headers.yaml
//    appendHeaders:
//      istio-custom-header: user-defined-value
type DestinationWeight struct {
	Destination IstioService `json:"destination"`
	Weight      int          `json:"weight"`
}

type IstioService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Domain    string `json:"domain"`
}

type Match struct {
	Request MatchRequest `json:"request"`
}

type MatchRequest struct {
	Headers Headers `json:"headers"`
}

type Headers struct {
	Authority MatchString `json:"authority"`
}

type MatchString struct {
	Exact  string `json:"exact,omitempty"`
	Regex  string `json:"regex,omitempty"`
	Prefix string `json:"prefix,omitempty"`
}

type RouteRuleSpec struct {
	Destination   IstioService        `json:"destination"`
	Match         Match               `json:"match,omitempty"`
	Route         []DestinationWeight `json:"route"`
	AppendHeaders map[string]string   `json:"appendHeaders"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RouteRuleList is a list of RouteRule resources
type RouteRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []RouteRule `json:"items"`
}
