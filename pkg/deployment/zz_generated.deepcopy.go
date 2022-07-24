//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2022 The Knative Authors

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

package deployment

import (
	sets "k8s.io/apimachinery/pkg/util/sets"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Config) DeepCopyInto(out *Config) {
	*out = *in
	if in.RegistriesSkippingTagResolving != nil {
		in, out := &in.RegistriesSkippingTagResolving, &out.RegistriesSkippingTagResolving
		*out = make(sets.String, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.QueueSidecarCPURequest != nil {
		in, out := &in.QueueSidecarCPURequest, &out.QueueSidecarCPURequest
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.QueueSidecarCPULimit != nil {
		in, out := &in.QueueSidecarCPULimit, &out.QueueSidecarCPULimit
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.QueueSidecarMemoryRequest != nil {
		in, out := &in.QueueSidecarMemoryRequest, &out.QueueSidecarMemoryRequest
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.QueueSidecarMemoryLimit != nil {
		in, out := &in.QueueSidecarMemoryLimit, &out.QueueSidecarMemoryLimit
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.QueueSidecarEphemeralStorageRequest != nil {
		in, out := &in.QueueSidecarEphemeralStorageRequest, &out.QueueSidecarEphemeralStorageRequest
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.QueueSidecarEphemeralStorageLimit != nil {
		in, out := &in.QueueSidecarEphemeralStorageLimit, &out.QueueSidecarEphemeralStorageLimit
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.QPExtensionAnnotations != nil {
		in, out := &in.QPExtensionAnnotations, &out.QPExtensionAnnotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Config.
func (in *Config) DeepCopy() *Config {
	if in == nil {
		return nil
	}
	out := new(Config)
	in.DeepCopyInto(out)
	return out
}
