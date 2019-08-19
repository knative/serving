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

package v1beta1

import (
	conversion "k8s.io/apimachinery/pkg/conversion"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	"knative.dev/serving/pkg/apis/internalversions/serving"
)

func Convert_serving_ConfigurationSpec_To_v1beta1_ConfigurationSpec(in *serving.ConfigurationSpec, out *ConfigurationSpec, s conversion.Scope) error {
	return Convert_serving_RevisionTemplateSpec_To_v1beta1_RevisionTemplateSpec(in.Template, &out.Template, s)
}

func Convert_v1beta1_ConfigurationSpec_To_serving_ConfigurationSpec(in *ConfigurationSpec, out *serving.ConfigurationSpec, s conversion.Scope) error {
	out.Template = &serving.RevisionTemplateSpec{}
	return Convert_v1beta1_RevisionTemplateSpec_To_serving_RevisionTemplateSpec(&in.Template, out.Template, s)
}

func Convert_serving_RevisionTemplateSpec_To_v1beta1_RevisionTemplateSpec(in *serving.RevisionTemplateSpec, out *RevisionTemplateSpec, s conversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	return Convert_serving_RevisionSpec_To_v1beta1_RevisionSpec(&in.Spec, &out.Spec, s)
}

func Convert_v1beta1_RevisionTemplateSpec_To_serving_RevisionTemplateSpec(in *RevisionTemplateSpec, out *serving.RevisionTemplateSpec, s conversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	return Convert_v1beta1_RevisionSpec_To_serving_RevisionSpec(&in.Spec, &out.Spec, s)
}

func Convert_serving_RevisionSpec_To_v1beta1_RevisionSpec(in *serving.RevisionSpec, out *RevisionSpec, s conversion.Scope) error {
	out.PodSpec = in.PodSpec
	out.ContainerConcurrency = RevisionContainerConcurrencyType(in.ContainerConcurrency)
	out.TimeoutSeconds = in.TimeoutSeconds
	return nil
}

func Convert_v1beta1_RevisionSpec_To_serving_RevisionSpec(in *RevisionSpec, out *serving.RevisionSpec, s conversion.Scope) error {
	out.PodSpec = in.PodSpec
	out.ContainerConcurrency = serving.RevisionContainerConcurrencyType(in.ContainerConcurrency)
	out.TimeoutSeconds = in.TimeoutSeconds
	return nil
}

func Convert_serving_TrafficTarget_To_v1beta1_TrafficTarget(in *serving.TrafficTarget, out *TrafficTarget, s conversion.Scope) error {
	out.Tag = in.Tag
	out.RevisionName = in.RevisionName
	out.ConfigurationName = in.ConfigurationName
	out.LatestRevision = in.LatestRevision
	out.Percent = in.Percent
	out.URL = in.URL
	return nil
}

func Convert_v1beta1_TrafficTarget_To_serving_TrafficTarget(in *TrafficTarget, out *serving.TrafficTarget, s conversion.Scope) error {
	out.Tag = in.Tag
	out.RevisionName = in.RevisionName
	out.ConfigurationName = in.ConfigurationName
	out.LatestRevision = in.LatestRevision
	out.Percent = in.Percent
	out.URL = in.URL
	return nil
}

func Convert_serving_RouteSpec_To_v1beta1_RouteSpec(in *serving.RouteSpec, out *RouteSpec, s conversion.Scope) error {
	out.Traffic = make([]TrafficTarget, len(in.Traffic))

	for i, target := range in.Traffic {
		// TODO(dprotaso) Handle error even though there is no error
		Convert_serving_TrafficTarget_To_v1beta1_TrafficTarget(&target, &out.Traffic[i], s)
	}

	return nil
}

func Convert_v1beta1_RouteSpec_To_serving_RouteSpec(in *RouteSpec, out *serving.RouteSpec, s conversion.Scope) error {
	out.Traffic = make([]serving.TrafficTarget, len(in.Traffic))

	for i, target := range in.Traffic {
		// TODO(dprotaso) Handle error even though there is no error
		Convert_v1beta1_TrafficTarget_To_serving_TrafficTarget(&target, &out.Traffic[i], s)
	}

	return nil
}

func Convert_serving_RouteStatusFields_To_v1beta1_RouteStatusFields(in *serving.RouteStatusFields, out *RouteStatusFields, s conversion.Scope) error {
	out.URL = in.URL
	if in.Address != nil {
		out.Address = &in.Address.Addressable
	}
	out.Traffic = make([]TrafficTarget, len(in.Traffic))

	for i, target := range in.Traffic {
		// TODO(dprotaso) Handle error even though there is no error
		Convert_serving_TrafficTarget_To_v1beta1_TrafficTarget(&target, &out.Traffic[i], s)
	}
	return nil
}

func Convert_v1beta1_RouteStatusFields_To_serving_RouteStatusFields(in *RouteStatusFields, out *serving.RouteStatusFields, s conversion.Scope) error {
	out.URL = in.URL

	if in.Address != nil {
		out.Address = &duckv1alpha1.Addressable{
			Addressable: *in.Address,
		}
	}
	out.Traffic = make([]serving.TrafficTarget, len(in.Traffic))

	for i, target := range in.Traffic {
		// TODO(dprotaso) Handle error even though there is no error
		Convert_v1beta1_TrafficTarget_To_serving_TrafficTarget(&target, &out.Traffic[i], s)
	}
	return nil
}

func Convert_v1beta1_ServiceSpec_To_serving_ServiceSpec(in *ServiceSpec, out *serving.ServiceSpec, s conversion.Scope) error {
	Convert_v1beta1_ConfigurationSpec_To_serving_ConfigurationSpec(&in.ConfigurationSpec, &out.ConfigurationSpec, s)
	Convert_v1beta1_RouteSpec_To_serving_RouteSpec(&in.RouteSpec, &out.RouteSpec, s)
	return nil
}

func Convert_serving_ServiceSpec_To_v1beta1_ServiceSpec(in *serving.ServiceSpec, out *ServiceSpec, s conversion.Scope) error {
	Convert_serving_ConfigurationSpec_To_v1beta1_ConfigurationSpec(&in.ConfigurationSpec, &out.ConfigurationSpec, s)
	Convert_serving_RouteSpec_To_v1beta1_RouteSpec(&in.RouteSpec, &out.RouteSpec, s)
	return nil
}
