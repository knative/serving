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

	"github.com/knative/pkg/apis"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

// ClusterIngress is a collection of rules that allow inbound connections to reach the
// endpoints defined by a backend. An ClusterIngress can be configured to give services
// externally-reachable urls, load balance traffic offer name based virtual hosting etc.
//
// This is heavily based on K8s Ingress https://godoc.org/k8s.io/api/extensions/v1beta1#Ingress
// which some highlighted modifications.
type ClusterIngress struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the ClusterIngress.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec ClusterIngressSpec `json:"spec,omitempty"`

	// Status is the current state of the ClusterIngress.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Status ClusterIngressStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterIngressList is a collection of ClusterIngress.
type ClusterIngressList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ClusterIngress.
	Items []ClusterIngress `json:"items"`
}

// ClusterIngressSpec describes the ClusterIngress the user wishes to exist.
type ClusterIngressSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// TLS configuration. Currently the ClusterIngress only supports a single TLS
	// port, 443. If multiple members of this list specify different hosts, they
	// will be multiplexed on the same port according to the hostname specified
	// through the SNI TLS extension, if the ingress controller fulfilling the
	// ingress supports SNI.
	// +optional
	TLS []ClusterIngressTLS `json:"tls,omitempty"`

	// A list of host rules used to configure the ClusterIngress. If unspecified, or
	// no rule matches, all traffic is sent to the default backend.
	// +optional
	Rules []ClusterIngressRule `json:"rules,omitempty"`
}

// ClusterIngressTLS describes the transport layer security associated with an ClusterIngress.
type ClusterIngressTLS struct {
	// Hosts are a list of hosts included in the TLS certificate. The values in
	// this list must match the name/s used in the tlsSecret. Defaults to the
	// wildcard host setting for the loadbalancer controller fulfilling this
	// ClusterIngress, if left unspecified.
	// +optional
	Hosts []string `json:"hosts,omitempty"`
	// SecretName is the name of the secret used to terminate SSL traffic.
	// Field is left optional to allow SSL routing based on SNI hostname alone.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// SecretNamespace is the namespace of the secret used to terminate SSL traffic.
	// Field is left optional to allow SSL routing based on SNI hostname alone.
	// +optional
	SecretNamespace string `json:"secretNamespace,omitempty"`

	// ServerCertificate identifies the certificate filename in the secret.
	// Defaults to `tls.cert`.
	// +optional
	ServerCertificate string `json:"serverCertificate,omitempty"`

	// PrivateKey identifies the private key filename in the secret.
	// Defaults to `tls.key`.
	// +optional
	PrivateKey string `json:"privateKey,omitempty"`
}

// ConditionType represents a ClusterIngress condition value
const (
	// ClusterIngressConditionReady is set when the clusterIngress is configured
	// and has a ready VirtualService.
	ClusterIngressConditionReady = duckv1alpha1.ConditionReady

	// ClusterIngressConditionNetworkConfigured is set when the ClusterIngress's underlying
	// network programming has been configured.
	ClusterIngressConditionNetworkConfigured duckv1alpha1.ConditionType = "NetworkConfigured"

	// ClusterIngressConditionLoadBalancerReady is set when the ClusterIngress has
	// a ready LoadBalancer.
	ClusterIngressConditionLoadBalancerReady duckv1alpha1.ConditionType = "LoadBalancerReady"
)

var clusterIngressCondSet = duckv1alpha1.NewLivingConditionSet(
	ClusterIngressConditionNetworkConfigured,
	ClusterIngressConditionLoadBalancerReady)

// ClusterIngressStatus describe the current state of the ClusterIngress.
type ClusterIngressStatus struct {
	// +optional
	Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`
	// LoadBalancer contains the current status of the load-balancer.
	// +optional
	LoadBalancer *LoadBalancerStatus `json:"loadBalancer,omitempty"`
}

// LoadBalancerStatus represents the status of a load-balancer.
type LoadBalancerStatus struct {
	// Ingress is a list containing ingress points for the load-balancer.
	// Traffic intended for the service should be sent to these ingress points.
	// +optional
	Ingress []LoadBalancerIngress `json:"ingress,omitempty" protobuf:"bytes,1,rep,name=ingress"`
}

// LoadBalancerIngress represents the status of a load-balancer ingress point:
// traffic intended for the service should be sent to an ingress point.
type LoadBalancerIngress struct {
	// IP is set for load-balancer ingress points that are IP based
	// (typically GCE or OpenStack load-balancers)
	// +optional
	IP string `json:"ip,omitempty" protobuf:"bytes,1,opt,name=ip"`

	// Domain is set for load-balancer ingress points that are DNS based
	// (typically AWS load-balancers)
	// +optional
	Domain string `json:"hostname,omitempty" protobuf:"bytes,2,opt,name=hostname"`

	// DomainInternal is set if there is a cluster-local DNS name to access the Ingress.
	//
	// NOTE: This differs from K8s Ingress, since we also desire to have a cluster-local
	//       DNS name to allow routing in case of not having a mesh.
	//
	// +optional
	DomainInternal string `json:"hostname,omitempty"`
}

// ClusterIngressRule represents the rules mapping the paths under a specified host to
// the related backend services. Incoming requests are first evaluated for a host
// match, then routed to the backend associated with the matching ClusterIngressRuleValue.
type ClusterIngressRule struct {
	// Host is the fully qualified domain name of a network host, as defined
	// by RFC 3986. Note the following deviations from the "host" part of the
	// URI as defined in the RFC:
	// 1. IPs are not allowed. Currently a ClusterIngressRuleV can only apply to the
	//	  IP in the Spec of the parent ClusterIngress.
	// 2. The `:` delimiter is not respected because ports are not allowed.
	//	  Currently the port of an ClusterIngress is implicitly :80 for http and
	//	  :443 for https.
	// Both these may change in the future.
	// Incoming requests are matched against the host before the ClusterIngressRuleValue.
	// If the host is unspecified, the ClusterIngress routes all traffic based on the
	// specified ClusterIngressRuleValue.
	// +optional
	Hosts []string `json:"hosts,omitempty"`

	// ClusterIngressRuleValue represents a rule to route requests for this ClusterIngressRule.
	// If unspecified, the rule defaults to a http catch-all. Whether that sends
	// just traffic matching the host to the default backend or all traffic to the
	// default backend, is left to the controller fulfilling the ClusterIngress. Http is
	// currently the only supported ClusterIngressRuleValue.
	//
	// NOTE: We could inline ClusterIngressRuleValue into the parent struct, but grouping things
	//       here to better highlight the boundary of the "OneOf" restriction.
	// +optional
	ClusterIngressRuleValue `json:",inline,omitempty"`
}

// ClusterIngressRuleValue represents a rule to apply against incoming requests. If the
// rule is satisfied, the request is routed to the specified backend. Currently
// mixing different types of rules in a single ClusterIngress is disallowed, so exactly
// one of the following must be set.
type ClusterIngressRuleValue struct {

	// HTTP is the only supported ClusterIngressRuleValue currently so
	// it is not optional.  However, in the future when we have others
	// we may it will become so.  +optional
	HTTP *HTTPClusterIngressRuleValue `json:"http,omitempty"`
}

// HTTPClusterIngressRuleValue is a list of http selectors pointing to backends.
// In the example: http://<host>/<path>?<searchpart> -> backend where
// where parts of the url correspond to RFC 3986, this resource will be used
// to match against everything after the last '/' and before the first '?'
// or '#'.
type HTTPClusterIngressRuleValue struct {
	// A collection of paths that map requests to backends.
	Paths []HTTPClusterIngressPath `json:"paths"`
	// TODO: Consider adding fields for ingress-type specific global
	// options usable by a loadbalancer, like http keep-alive.
}

// HTTPClusterIngressPath associates a path regex with a backend. Incoming urls matching
// the path are forwarded to the backend.
type HTTPClusterIngressPath struct {
	// Path is an extended POSIX regex as defined by IEEE Std 1003.1,
	// (i.e this follows the egrep/unix syntax, not the perl syntax)
	// matched against the path of an incoming request. Currently it can
	// contain characters disallowed from the conventional "path"
	// part of a URL as defined by RFC 3986. Paths must begin with
	// a '/'. If unspecified, the path defaults to a catch all sending
	// traffic to the backend.
	// +optional
	Path string `json:"path,omitempty"`

	// Splits defines the referenced service endpoints to which the traffic
	// will be forwarded to.
	Splits []ClusterIngressBackendSplit `json:"splits"`

	// AppendHeaders allow specifying additional HTTP headers to add
	// before forwarding a request to the destination service.
	//
	// NOTE: This differs from K8s Ingress which doesn't allow header appending.
	// +optional
	AppendHeaders map[string]string `json:"appendHeaders,omitempty"`

	// Timeout for HTTP requests.
	//
	// NOTE: This differs from K8s Ingress which doesn't allow setting timeouts.
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Retry policy for HTTP requests.
	//
	// NOTE: This differs from K8s Ingress which doesn't allow retry settings.
	// +optional
	Retries *HTTPRetry `json:"retries,omitempty"`
}

// HTTPRetry describes the retry policy to use when a HTTP request fails.
type HTTPRetry struct {
	// Number of retries for a given request.
	Attempts int `json:"attempts"`

	// Timeout per retry attempt for a given request. format: 1h/1m/1s/1ms. MUST BE >=1ms.
	PerTryTimeout *metav1.Duration `json:"perTryTimeout"`
}

// ClusterIngressBackend describes all endpoints for a given service and port.
type ClusterIngressBackend struct {
	// Specifies the namespace of the referenced service.
	//
	// NOTE: This differs from K8s Ingress to allow routing to different namespaces.
	ServiceNamespace string `json:"serviceNamespace"`

	// Specifies the name of the referenced service.
	ServiceName string `json:"serviceName"`

	// Specifies the port of the referenced service.
	ServicePort intstr.IntOrString `json:"servicePort"`
}

// ClusterIngressBackend describes all endpoints for a given service and port.
type ClusterIngressBackendSplit struct {
	// Specifies the backend receiving the traffic split.
	Backend *ClusterIngressBackend `json:"backend"`

	// Specifies the split percentage, a number between 0 and 100.  If
	// only one split is specified, we default to 100.
	//
	// NOTE: This differs from K8s Ingress to allow percentage split.
	Percent int `json:"percent,omitempty"`
}

var _ apis.Validatable = (*ClusterIngress)(nil)
var _ apis.Defaultable = (*ClusterIngress)(nil)

func (ci *ClusterIngress) GetGeneration() int64 {
	return ci.Spec.Generation
}

func (ci *ClusterIngress) SetGeneration(generation int64) {
	ci.Spec.Generation = generation
}

func (ci *ClusterIngress) GetSpecJSON() ([]byte, error) {
	return json.Marshal(ci.Spec)
}

func (ci *ClusterIngress) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("ClusterIngress")
}

// GetConditions returns the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (cis *ClusterIngressStatus) GetConditions() duckv1alpha1.Conditions {
	return cis.Conditions
}

// SetConditions sets the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (cis *ClusterIngressStatus) SetConditions(conditions duckv1alpha1.Conditions) {
	cis.Conditions = conditions
}

func (cis *ClusterIngressStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return clusterIngressCondSet.Manage(cis).GetCondition(t)
}

func (cis *ClusterIngressStatus) InitializeConditions() {
	clusterIngressCondSet.Manage(cis).InitializeConditions()
}

func (cis *ClusterIngressStatus) MarkNetworkConfigured() {
	clusterIngressCondSet.Manage(cis).MarkTrue(ClusterIngressConditionNetworkConfigured)
}

// MarkLoadBalancerReady marks the Ingress with ClusterIngressConditionLoadBalancerReady,
// and also populate the address of the load balancer.  We allow multiple load balancers
// address, but currently our Istio implementation only needs/has one so this signature
// is better taking []LoadBalancerIngress, as we can make sure exactly one address is
// provided.
func (cis *ClusterIngressStatus) MarkLoadBalancerReady(lb LoadBalancerIngress) {
	cis.LoadBalancer = &LoadBalancerStatus{
		Ingress: []LoadBalancerIngress{lb},
	}
	clusterIngressCondSet.Manage(cis).MarkTrue(ClusterIngressConditionLoadBalancerReady)
}

// IsReady looks at the conditions and if the Status has a condition
// ClusterIngressConditionReady returns true if ConditionStatus is True
func (cis *ClusterIngressStatus) IsReady() bool {
	return clusterIngressCondSet.Manage(cis).IsHappy()
}
