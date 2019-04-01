// +build !ignore_autogenerated

/*
Copyright 2018 The Knative Authors

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

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha3

import (
	v1alpha1 "github.com/knative/pkg/apis/istio/common/v1alpha1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConnectionPoolSettings) DeepCopyInto(out *ConnectionPoolSettings) {
	*out = *in
	if in.TCP != nil {
		in, out := &in.TCP, &out.TCP
		*out = new(TCPSettings)
		**out = **in
	}
	if in.HTTP != nil {
		in, out := &in.HTTP, &out.HTTP
		*out = new(HTTPSettings)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConnectionPoolSettings.
func (in *ConnectionPoolSettings) DeepCopy() *ConnectionPoolSettings {
	if in == nil {
		return nil
	}
	out := new(ConnectionPoolSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsistentHashLB) DeepCopyInto(out *ConsistentHashLB) {
	*out = *in
	if in.HTTPCookie != nil {
		in, out := &in.HTTPCookie, &out.HTTPCookie
		*out = new(HTTPCookie)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsistentHashLB.
func (in *ConsistentHashLB) DeepCopy() *ConsistentHashLB {
	if in == nil {
		return nil
	}
	out := new(ConsistentHashLB)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CorsPolicy) DeepCopyInto(out *CorsPolicy) {
	*out = *in
	if in.AllowOrigin != nil {
		in, out := &in.AllowOrigin, &out.AllowOrigin
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.AllowMethods != nil {
		in, out := &in.AllowMethods, &out.AllowMethods
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.AllowHeaders != nil {
		in, out := &in.AllowHeaders, &out.AllowHeaders
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ExposeHeaders != nil {
		in, out := &in.ExposeHeaders, &out.ExposeHeaders
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CorsPolicy.
func (in *CorsPolicy) DeepCopy() *CorsPolicy {
	if in == nil {
		return nil
	}
	out := new(CorsPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Destination) DeepCopyInto(out *Destination) {
	*out = *in
	out.Port = in.Port
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Destination.
func (in *Destination) DeepCopy() *Destination {
	if in == nil {
		return nil
	}
	out := new(Destination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DestinationRule) DeepCopyInto(out *DestinationRule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DestinationRule.
func (in *DestinationRule) DeepCopy() *DestinationRule {
	if in == nil {
		return nil
	}
	out := new(DestinationRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DestinationRule) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DestinationRuleList) DeepCopyInto(out *DestinationRuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DestinationRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DestinationRuleList.
func (in *DestinationRuleList) DeepCopy() *DestinationRuleList {
	if in == nil {
		return nil
	}
	out := new(DestinationRuleList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DestinationRuleList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DestinationRuleSpec) DeepCopyInto(out *DestinationRuleSpec) {
	*out = *in
	if in.TrafficPolicy != nil {
		in, out := &in.TrafficPolicy, &out.TrafficPolicy
		*out = new(TrafficPolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.Subsets != nil {
		in, out := &in.Subsets, &out.Subsets
		*out = make([]Subset, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DestinationRuleSpec.
func (in *DestinationRuleSpec) DeepCopy() *DestinationRuleSpec {
	if in == nil {
		return nil
	}
	out := new(DestinationRuleSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DestinationWeight) DeepCopyInto(out *DestinationWeight) {
	*out = *in
	out.Destination = in.Destination
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DestinationWeight.
func (in *DestinationWeight) DeepCopy() *DestinationWeight {
	if in == nil {
		return nil
	}
	out := new(DestinationWeight)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Gateway) DeepCopyInto(out *Gateway) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Gateway.
func (in *Gateway) DeepCopy() *Gateway {
	if in == nil {
		return nil
	}
	out := new(Gateway)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Gateway) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GatewayList) DeepCopyInto(out *GatewayList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Gateway, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GatewayList.
func (in *GatewayList) DeepCopy() *GatewayList {
	if in == nil {
		return nil
	}
	out := new(GatewayList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GatewayList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GatewaySpec) DeepCopyInto(out *GatewaySpec) {
	*out = *in
	if in.Servers != nil {
		in, out := &in.Servers, &out.Servers
		*out = make([]Server, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GatewaySpec.
func (in *GatewaySpec) DeepCopy() *GatewaySpec {
	if in == nil {
		return nil
	}
	out := new(GatewaySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPCookie) DeepCopyInto(out *HTTPCookie) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPCookie.
func (in *HTTPCookie) DeepCopy() *HTTPCookie {
	if in == nil {
		return nil
	}
	out := new(HTTPCookie)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPFaultInjection) DeepCopyInto(out *HTTPFaultInjection) {
	*out = *in
	if in.Delay != nil {
		in, out := &in.Delay, &out.Delay
		*out = new(InjectDelay)
		**out = **in
	}
	if in.Abort != nil {
		in, out := &in.Abort, &out.Abort
		*out = new(InjectAbort)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPFaultInjection.
func (in *HTTPFaultInjection) DeepCopy() *HTTPFaultInjection {
	if in == nil {
		return nil
	}
	out := new(HTTPFaultInjection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPMatchRequest) DeepCopyInto(out *HTTPMatchRequest) {
	*out = *in
	if in.URI != nil {
		in, out := &in.URI, &out.URI
		*out = new(v1alpha1.StringMatch)
		**out = **in
	}
	if in.Scheme != nil {
		in, out := &in.Scheme, &out.Scheme
		*out = new(v1alpha1.StringMatch)
		**out = **in
	}
	if in.Method != nil {
		in, out := &in.Method, &out.Method
		*out = new(v1alpha1.StringMatch)
		**out = **in
	}
	if in.Authority != nil {
		in, out := &in.Authority, &out.Authority
		*out = new(v1alpha1.StringMatch)
		**out = **in
	}
	if in.Headers != nil {
		in, out := &in.Headers, &out.Headers
		*out = make(map[string]v1alpha1.StringMatch, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.SourceLabels != nil {
		in, out := &in.SourceLabels, &out.SourceLabels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Gateways != nil {
		in, out := &in.Gateways, &out.Gateways
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPMatchRequest.
func (in *HTTPMatchRequest) DeepCopy() *HTTPMatchRequest {
	if in == nil {
		return nil
	}
	out := new(HTTPMatchRequest)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPRedirect) DeepCopyInto(out *HTTPRedirect) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPRedirect.
func (in *HTTPRedirect) DeepCopy() *HTTPRedirect {
	if in == nil {
		return nil
	}
	out := new(HTTPRedirect)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPRetry) DeepCopyInto(out *HTTPRetry) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPRetry.
func (in *HTTPRetry) DeepCopy() *HTTPRetry {
	if in == nil {
		return nil
	}
	out := new(HTTPRetry)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPRewrite) DeepCopyInto(out *HTTPRewrite) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPRewrite.
func (in *HTTPRewrite) DeepCopy() *HTTPRewrite {
	if in == nil {
		return nil
	}
	out := new(HTTPRewrite)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPRoute) DeepCopyInto(out *HTTPRoute) {
	*out = *in
	if in.Match != nil {
		in, out := &in.Match, &out.Match
		*out = make([]HTTPMatchRequest, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Route != nil {
		in, out := &in.Route, &out.Route
		*out = make([]DestinationWeight, len(*in))
		copy(*out, *in)
	}
	if in.Redirect != nil {
		in, out := &in.Redirect, &out.Redirect
		*out = new(HTTPRedirect)
		**out = **in
	}
	if in.Rewrite != nil {
		in, out := &in.Rewrite, &out.Rewrite
		*out = new(HTTPRewrite)
		**out = **in
	}
	if in.Retries != nil {
		in, out := &in.Retries, &out.Retries
		*out = new(HTTPRetry)
		**out = **in
	}
	if in.Fault != nil {
		in, out := &in.Fault, &out.Fault
		*out = new(HTTPFaultInjection)
		(*in).DeepCopyInto(*out)
	}
	if in.Mirror != nil {
		in, out := &in.Mirror, &out.Mirror
		*out = new(Destination)
		**out = **in
	}
	if in.AppendHeaders != nil {
		in, out := &in.AppendHeaders, &out.AppendHeaders
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.RemoveResponseHeaders != nil {
		in, out := &in.RemoveResponseHeaders, &out.RemoveResponseHeaders
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.CorsPolicy != nil {
		in, out := &in.CorsPolicy, &out.CorsPolicy
		*out = new(CorsPolicy)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPRoute.
func (in *HTTPRoute) DeepCopy() *HTTPRoute {
	if in == nil {
		return nil
	}
	out := new(HTTPRoute)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPSettings) DeepCopyInto(out *HTTPSettings) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPSettings.
func (in *HTTPSettings) DeepCopy() *HTTPSettings {
	if in == nil {
		return nil
	}
	out := new(HTTPSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InjectAbort) DeepCopyInto(out *InjectAbort) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InjectAbort.
func (in *InjectAbort) DeepCopy() *InjectAbort {
	if in == nil {
		return nil
	}
	out := new(InjectAbort)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InjectDelay) DeepCopyInto(out *InjectDelay) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InjectDelay.
func (in *InjectDelay) DeepCopy() *InjectDelay {
	if in == nil {
		return nil
	}
	out := new(InjectDelay)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *L4MatchAttributes) DeepCopyInto(out *L4MatchAttributes) {
	*out = *in
	if in.DestinationSubnets != nil {
		in, out := &in.DestinationSubnets, &out.DestinationSubnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.SourceLabels != nil {
		in, out := &in.SourceLabels, &out.SourceLabels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Gateways != nil {
		in, out := &in.Gateways, &out.Gateways
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new L4MatchAttributes.
func (in *L4MatchAttributes) DeepCopy() *L4MatchAttributes {
	if in == nil {
		return nil
	}
	out := new(L4MatchAttributes)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LoadBalancerSettings) DeepCopyInto(out *LoadBalancerSettings) {
	*out = *in
	if in.ConsistentHash != nil {
		in, out := &in.ConsistentHash, &out.ConsistentHash
		*out = new(ConsistentHashLB)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LoadBalancerSettings.
func (in *LoadBalancerSettings) DeepCopy() *LoadBalancerSettings {
	if in == nil {
		return nil
	}
	out := new(LoadBalancerSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OutlierDetection) DeepCopyInto(out *OutlierDetection) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OutlierDetection.
func (in *OutlierDetection) DeepCopy() *OutlierDetection {
	if in == nil {
		return nil
	}
	out := new(OutlierDetection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Port) DeepCopyInto(out *Port) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Port.
func (in *Port) DeepCopy() *Port {
	if in == nil {
		return nil
	}
	out := new(Port)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PortSelector) DeepCopyInto(out *PortSelector) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PortSelector.
func (in *PortSelector) DeepCopy() *PortSelector {
	if in == nil {
		return nil
	}
	out := new(PortSelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PortTrafficPolicy) DeepCopyInto(out *PortTrafficPolicy) {
	*out = *in
	out.Port = in.Port
	if in.LoadBalancer != nil {
		in, out := &in.LoadBalancer, &out.LoadBalancer
		*out = new(LoadBalancerSettings)
		(*in).DeepCopyInto(*out)
	}
	if in.ConnectionPool != nil {
		in, out := &in.ConnectionPool, &out.ConnectionPool
		*out = new(ConnectionPoolSettings)
		(*in).DeepCopyInto(*out)
	}
	if in.OutlierDetection != nil {
		in, out := &in.OutlierDetection, &out.OutlierDetection
		*out = new(OutlierDetection)
		**out = **in
	}
	if in.TLS != nil {
		in, out := &in.TLS, &out.TLS
		*out = new(TLSSettings)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PortTrafficPolicy.
func (in *PortTrafficPolicy) DeepCopy() *PortTrafficPolicy {
	if in == nil {
		return nil
	}
	out := new(PortTrafficPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Server) DeepCopyInto(out *Server) {
	*out = *in
	out.Port = in.Port
	if in.Hosts != nil {
		in, out := &in.Hosts, &out.Hosts
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.TLS != nil {
		in, out := &in.TLS, &out.TLS
		*out = new(TLSOptions)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Server.
func (in *Server) DeepCopy() *Server {
	if in == nil {
		return nil
	}
	out := new(Server)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Subset) DeepCopyInto(out *Subset) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.TrafficPolicy != nil {
		in, out := &in.TrafficPolicy, &out.TrafficPolicy
		*out = new(TrafficPolicy)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Subset.
func (in *Subset) DeepCopy() *Subset {
	if in == nil {
		return nil
	}
	out := new(Subset)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TCPRoute) DeepCopyInto(out *TCPRoute) {
	*out = *in
	if in.Match != nil {
		in, out := &in.Match, &out.Match
		*out = make([]L4MatchAttributes, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Route != nil {
		in, out := &in.Route, &out.Route
		*out = make([]DestinationWeight, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TCPRoute.
func (in *TCPRoute) DeepCopy() *TCPRoute {
	if in == nil {
		return nil
	}
	out := new(TCPRoute)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TCPSettings) DeepCopyInto(out *TCPSettings) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TCPSettings.
func (in *TCPSettings) DeepCopy() *TCPSettings {
	if in == nil {
		return nil
	}
	out := new(TCPSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TLSMatchAttributes) DeepCopyInto(out *TLSMatchAttributes) {
	*out = *in
	if in.SniHosts != nil {
		in, out := &in.SniHosts, &out.SniHosts
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.DestinationSubnets != nil {
		in, out := &in.DestinationSubnets, &out.DestinationSubnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.SourceLabels != nil {
		in, out := &in.SourceLabels, &out.SourceLabels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Gateways != nil {
		in, out := &in.Gateways, &out.Gateways
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TLSMatchAttributes.
func (in *TLSMatchAttributes) DeepCopy() *TLSMatchAttributes {
	if in == nil {
		return nil
	}
	out := new(TLSMatchAttributes)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TLSOptions) DeepCopyInto(out *TLSOptions) {
	*out = *in
	if in.SubjectAltNames != nil {
		in, out := &in.SubjectAltNames, &out.SubjectAltNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TLSOptions.
func (in *TLSOptions) DeepCopy() *TLSOptions {
	if in == nil {
		return nil
	}
	out := new(TLSOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TLSRoute) DeepCopyInto(out *TLSRoute) {
	*out = *in
	if in.Match != nil {
		in, out := &in.Match, &out.Match
		*out = make([]TLSMatchAttributes, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Route != nil {
		in, out := &in.Route, &out.Route
		*out = make([]DestinationWeight, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TLSRoute.
func (in *TLSRoute) DeepCopy() *TLSRoute {
	if in == nil {
		return nil
	}
	out := new(TLSRoute)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TLSSettings) DeepCopyInto(out *TLSSettings) {
	*out = *in
	if in.SubjectAltNames != nil {
		in, out := &in.SubjectAltNames, &out.SubjectAltNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TLSSettings.
func (in *TLSSettings) DeepCopy() *TLSSettings {
	if in == nil {
		return nil
	}
	out := new(TLSSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TrafficPolicy) DeepCopyInto(out *TrafficPolicy) {
	*out = *in
	if in.LoadBalancer != nil {
		in, out := &in.LoadBalancer, &out.LoadBalancer
		*out = new(LoadBalancerSettings)
		(*in).DeepCopyInto(*out)
	}
	if in.ConnectionPool != nil {
		in, out := &in.ConnectionPool, &out.ConnectionPool
		*out = new(ConnectionPoolSettings)
		(*in).DeepCopyInto(*out)
	}
	if in.OutlierDetection != nil {
		in, out := &in.OutlierDetection, &out.OutlierDetection
		*out = new(OutlierDetection)
		**out = **in
	}
	if in.TLS != nil {
		in, out := &in.TLS, &out.TLS
		*out = new(TLSSettings)
		(*in).DeepCopyInto(*out)
	}
	if in.PortLevelSettings != nil {
		in, out := &in.PortLevelSettings, &out.PortLevelSettings
		*out = make([]PortTrafficPolicy, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TrafficPolicy.
func (in *TrafficPolicy) DeepCopy() *TrafficPolicy {
	if in == nil {
		return nil
	}
	out := new(TrafficPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VirtualService) DeepCopyInto(out *VirtualService) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VirtualService.
func (in *VirtualService) DeepCopy() *VirtualService {
	if in == nil {
		return nil
	}
	out := new(VirtualService)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *VirtualService) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VirtualServiceList) DeepCopyInto(out *VirtualServiceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]VirtualService, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VirtualServiceList.
func (in *VirtualServiceList) DeepCopy() *VirtualServiceList {
	if in == nil {
		return nil
	}
	out := new(VirtualServiceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *VirtualServiceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VirtualServiceSpec) DeepCopyInto(out *VirtualServiceSpec) {
	*out = *in
	if in.Hosts != nil {
		in, out := &in.Hosts, &out.Hosts
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Gateways != nil {
		in, out := &in.Gateways, &out.Gateways
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.HTTP != nil {
		in, out := &in.HTTP, &out.HTTP
		*out = make([]HTTPRoute, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TCP != nil {
		in, out := &in.TCP, &out.TCP
		*out = make([]TCPRoute, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TLS != nil {
		in, out := &in.TLS, &out.TLS
		*out = make([]TLSRoute, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VirtualServiceSpec.
func (in *VirtualServiceSpec) DeepCopy() *VirtualServiceSpec {
	if in == nil {
		return nil
	}
	out := new(VirtualServiceSpec)
	in.DeepCopyInto(out)
	return out
}
