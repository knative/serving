// +build !ignore_autogenerated

/*
Copyright 2019 The Knative Authors

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

package v1alpha1

import (
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	v1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CannotConvertError) DeepCopyInto(out *CannotConvertError) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CannotConvertError.
func (in *CannotConvertError) DeepCopy() *CannotConvertError {
	if in == nil {
		return nil
	}
	out := new(CannotConvertError)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Configuration) DeepCopyInto(out *Configuration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Configuration.
func (in *Configuration) DeepCopy() *Configuration {
	if in == nil {
		return nil
	}
	out := new(Configuration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Configuration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConfigurationList) DeepCopyInto(out *ConfigurationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Configuration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConfigurationList.
func (in *ConfigurationList) DeepCopy() *ConfigurationList {
	if in == nil {
		return nil
	}
	out := new(ConfigurationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ConfigurationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConfigurationSpec) DeepCopyInto(out *ConfigurationSpec) {
	*out = *in
	if in.DeprecatedBuild != nil {
		in, out := &in.DeprecatedBuild, &out.DeprecatedBuild
		*out = new(RawExtension)
		(*in).DeepCopyInto(*out)
	}
	if in.DeprecatedRevisionTemplate != nil {
		in, out := &in.DeprecatedRevisionTemplate, &out.DeprecatedRevisionTemplate
		*out = new(RevisionTemplateSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Template != nil {
		in, out := &in.Template, &out.Template
		*out = new(RevisionTemplateSpec)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConfigurationSpec.
func (in *ConfigurationSpec) DeepCopy() *ConfigurationSpec {
	if in == nil {
		return nil
	}
	out := new(ConfigurationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConfigurationStatus) DeepCopyInto(out *ConfigurationStatus) {
	*out = *in
	in.Status.DeepCopyInto(&out.Status)
	out.ConfigurationStatusFields = in.ConfigurationStatusFields
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConfigurationStatus.
func (in *ConfigurationStatus) DeepCopy() *ConfigurationStatus {
	if in == nil {
		return nil
	}
	out := new(ConfigurationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConfigurationStatusFields) DeepCopyInto(out *ConfigurationStatusFields) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConfigurationStatusFields.
func (in *ConfigurationStatusFields) DeepCopy() *ConfigurationStatusFields {
	if in == nil {
		return nil
	}
	out := new(ConfigurationStatusFields)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManualType) DeepCopyInto(out *ManualType) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManualType.
func (in *ManualType) DeepCopy() *ManualType {
	if in == nil {
		return nil
	}
	out := new(ManualType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PinnedType) DeepCopyInto(out *PinnedType) {
	*out = *in
	in.Configuration.DeepCopyInto(&out.Configuration)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PinnedType.
func (in *PinnedType) DeepCopy() *PinnedType {
	if in == nil {
		return nil
	}
	out := new(PinnedType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RawExtension) DeepCopyInto(out *RawExtension) {
	*out = *in
	if in.Raw != nil {
		in, out := &in.Raw, &out.Raw
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
	if in.Object != nil {
		out.Object = in.Object.DeepCopyObject()
	}
	if in.BuildSpec != nil {
		in, out := &in.BuildSpec, &out.BuildSpec
		*out = new(buildv1alpha1.BuildSpec)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RawExtension.
func (in *RawExtension) DeepCopy() *RawExtension {
	if in == nil {
		return nil
	}
	out := new(RawExtension)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReleaseType) DeepCopyInto(out *ReleaseType) {
	*out = *in
	if in.Revisions != nil {
		in, out := &in.Revisions, &out.Revisions
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.Configuration.DeepCopyInto(&out.Configuration)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReleaseType.
func (in *ReleaseType) DeepCopy() *ReleaseType {
	if in == nil {
		return nil
	}
	out := new(ReleaseType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Revision) DeepCopyInto(out *Revision) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Revision.
func (in *Revision) DeepCopy() *Revision {
	if in == nil {
		return nil
	}
	out := new(Revision)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Revision) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RevisionList) DeepCopyInto(out *RevisionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Revision, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RevisionList.
func (in *RevisionList) DeepCopy() *RevisionList {
	if in == nil {
		return nil
	}
	out := new(RevisionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *RevisionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RevisionSpec) DeepCopyInto(out *RevisionSpec) {
	*out = *in
	in.RevisionSpec.DeepCopyInto(&out.RevisionSpec)
	if in.DeprecatedBuildRef != nil {
		in, out := &in.DeprecatedBuildRef, &out.DeprecatedBuildRef
		*out = new(v1.ObjectReference)
		**out = **in
	}
	if in.DeprecatedContainer != nil {
		in, out := &in.DeprecatedContainer, &out.DeprecatedContainer
		*out = new(v1.Container)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RevisionSpec.
func (in *RevisionSpec) DeepCopy() *RevisionSpec {
	if in == nil {
		return nil
	}
	out := new(RevisionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RevisionStatus) DeepCopyInto(out *RevisionStatus) {
	*out = *in
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RevisionStatus.
func (in *RevisionStatus) DeepCopy() *RevisionStatus {
	if in == nil {
		return nil
	}
	out := new(RevisionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RevisionTemplateSpec) DeepCopyInto(out *RevisionTemplateSpec) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RevisionTemplateSpec.
func (in *RevisionTemplateSpec) DeepCopy() *RevisionTemplateSpec {
	if in == nil {
		return nil
	}
	out := new(RevisionTemplateSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Route) DeepCopyInto(out *Route) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Route.
func (in *Route) DeepCopy() *Route {
	if in == nil {
		return nil
	}
	out := new(Route)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Route) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteList) DeepCopyInto(out *RouteList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Route, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteList.
func (in *RouteList) DeepCopy() *RouteList {
	if in == nil {
		return nil
	}
	out := new(RouteList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *RouteList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteSpec) DeepCopyInto(out *RouteSpec) {
	*out = *in
	if in.Traffic != nil {
		in, out := &in.Traffic, &out.Traffic
		*out = make([]TrafficTarget, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteSpec.
func (in *RouteSpec) DeepCopy() *RouteSpec {
	if in == nil {
		return nil
	}
	out := new(RouteSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteStatus) DeepCopyInto(out *RouteStatus) {
	*out = *in
	in.Status.DeepCopyInto(&out.Status)
	in.RouteStatusFields.DeepCopyInto(&out.RouteStatusFields)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteStatus.
func (in *RouteStatus) DeepCopy() *RouteStatus {
	if in == nil {
		return nil
	}
	out := new(RouteStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RouteStatusFields) DeepCopyInto(out *RouteStatusFields) {
	*out = *in
	if in.Address != nil {
		in, out := &in.Address, &out.Address
		*out = new(duckv1alpha1.Addressable)
		(*in).DeepCopyInto(*out)
	}
	if in.Traffic != nil {
		in, out := &in.Traffic, &out.Traffic
		*out = make([]TrafficTarget, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RouteStatusFields.
func (in *RouteStatusFields) DeepCopy() *RouteStatusFields {
	if in == nil {
		return nil
	}
	out := new(RouteStatusFields)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RunLatestType) DeepCopyInto(out *RunLatestType) {
	*out = *in
	in.Configuration.DeepCopyInto(&out.Configuration)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RunLatestType.
func (in *RunLatestType) DeepCopy() *RunLatestType {
	if in == nil {
		return nil
	}
	out := new(RunLatestType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Service) DeepCopyInto(out *Service) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Service.
func (in *Service) DeepCopy() *Service {
	if in == nil {
		return nil
	}
	out := new(Service)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Service) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceList) DeepCopyInto(out *ServiceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Service, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceList.
func (in *ServiceList) DeepCopy() *ServiceList {
	if in == nil {
		return nil
	}
	out := new(ServiceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceSpec) DeepCopyInto(out *ServiceSpec) {
	*out = *in
	if in.DeprecatedRunLatest != nil {
		in, out := &in.DeprecatedRunLatest, &out.DeprecatedRunLatest
		*out = new(RunLatestType)
		(*in).DeepCopyInto(*out)
	}
	if in.DeprecatedPinned != nil {
		in, out := &in.DeprecatedPinned, &out.DeprecatedPinned
		*out = new(PinnedType)
		(*in).DeepCopyInto(*out)
	}
	if in.DeprecatedManual != nil {
		in, out := &in.DeprecatedManual, &out.DeprecatedManual
		*out = new(ManualType)
		**out = **in
	}
	if in.DeprecatedRelease != nil {
		in, out := &in.DeprecatedRelease, &out.DeprecatedRelease
		*out = new(ReleaseType)
		(*in).DeepCopyInto(*out)
	}
	in.ConfigurationSpec.DeepCopyInto(&out.ConfigurationSpec)
	in.RouteSpec.DeepCopyInto(&out.RouteSpec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceSpec.
func (in *ServiceSpec) DeepCopy() *ServiceSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceStatus) DeepCopyInto(out *ServiceStatus) {
	*out = *in
	in.Status.DeepCopyInto(&out.Status)
	in.RouteStatusFields.DeepCopyInto(&out.RouteStatusFields)
	out.ConfigurationStatusFields = in.ConfigurationStatusFields
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceStatus.
func (in *ServiceStatus) DeepCopy() *ServiceStatus {
	if in == nil {
		return nil
	}
	out := new(ServiceStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TrafficTarget) DeepCopyInto(out *TrafficTarget) {
	*out = *in
	in.TrafficTarget.DeepCopyInto(&out.TrafficTarget)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TrafficTarget.
func (in *TrafficTarget) DeepCopy() *TrafficTarget {
	if in == nil {
		return nil
	}
	out := new(TrafficTarget)
	in.DeepCopyInto(out)
	return out
}
