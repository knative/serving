// +build !ignore_autogenerated

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

// This file was autogenerated by deepcopy-gen. Do not edit it manually!

package v1alpha2

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

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
func (in *Headers) DeepCopyInto(out *Headers) {
	*out = *in
	out.Authority = in.Authority
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Headers.
func (in *Headers) DeepCopy() *Headers {
	if in == nil {
		return nil
	}
	out := new(Headers)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IstioService) DeepCopyInto(out *IstioService) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IstioService.
func (in *IstioService) DeepCopy() *IstioService {
	if in == nil {
		return nil
	}
	out := new(IstioService)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Match) DeepCopyInto(out *Match) {
	*out = *in
	out.Request = in.Request
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Match.
func (in *Match) DeepCopy() *Match {
	if in == nil {
		return nil
	}
	out := new(Match)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MatchRequest) DeepCopyInto(out *MatchRequest) {
	*out = *in
	out.Headers = in.Headers
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MatchRequest.
func (in *MatchRequest) DeepCopy() *MatchRequest {
	if in == nil {
		return nil
	}
	out := new(MatchRequest)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MatchString) DeepCopyInto(out *MatchString) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MatchString.
func (in *MatchString) DeepCopy() *MatchString {
	if in == nil {
		return nil
	}
	out := new(MatchString)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteRule) DeepCopyInto(out *RouteRule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteRule.
func (in *RouteRule) DeepCopy() *RouteRule {
	if in == nil {
		return nil
	}
	out := new(RouteRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *RouteRule) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	} else {
		return nil
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteRuleList) DeepCopyInto(out *RouteRuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]RouteRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteRuleList.
func (in *RouteRuleList) DeepCopy() *RouteRuleList {
	if in == nil {
		return nil
	}
	out := new(RouteRuleList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *RouteRuleList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	} else {
		return nil
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteRuleSpec) DeepCopyInto(out *RouteRuleSpec) {
	*out = *in
	out.Destination = in.Destination
	if in.Match != nil {
		in, out := &in.Match, &out.Match
		if *in == nil {
			*out = nil
		} else {
			*out = new(Match)
			**out = **in
		}
	}
	if in.Route != nil {
		in, out := &in.Route, &out.Route
		*out = make([]DestinationWeight, len(*in))
		copy(*out, *in)
	}
	if in.AppendHeaders != nil {
		in, out := &in.AppendHeaders, &out.AppendHeaders
		if *in == nil {
			*out = nil
		} else {
			*out = new(map[string]string)
			if **in != nil {
				in, out := *in, *out
				*out = make(map[string]string, len(*in))
				for key, val := range *in {
					(*out)[key] = val
				}
			}
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteRuleSpec.
func (in *RouteRuleSpec) DeepCopy() *RouteRuleSpec {
	if in == nil {
		return nil
	}
	out := new(RouteRuleSpec)
	in.DeepCopyInto(out)
	return out
}
