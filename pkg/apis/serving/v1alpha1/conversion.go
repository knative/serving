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

package v1alpha1

import (
	conversion "k8s.io/apimachinery/pkg/conversion"
	"knative.dev/serving/pkg/apis/internalversions/serving"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

// TODO(dprotaso) v1alpha1 should just be typed alias of internal type

func Convert_serving_TrafficTarget_To_v1alpha1_TrafficTarget(in *serving.TrafficTarget, out *TrafficTarget, s conversion.Scope) error {
	out.DeprecatedName = in.DeprecatedName
	out.Tag = in.Tag
	out.RevisionName = in.RevisionName
	out.ConfigurationName = in.ConfigurationName
	out.LatestRevision = in.LatestRevision
	out.Percent = in.Percent
	out.URL = in.URL
	return nil
}

func Convert_v1alpha1_TrafficTarget_To_serving_TrafficTarget(in *TrafficTarget, out *serving.TrafficTarget, s conversion.Scope) error {
	out.DeprecatedName = in.DeprecatedName
	out.Tag = in.Tag
	out.RevisionName = in.RevisionName
	out.ConfigurationName = in.ConfigurationName
	out.LatestRevision = in.LatestRevision
	out.Percent = in.Percent
	out.URL = in.URL
	return nil
}

func Convert_serving_RevisionSpec_To_v1alpha1_RevisionSpec(in *serving.RevisionSpec, out *RevisionSpec, s conversion.Scope) error {
	// v1beta1 fields
	out.PodSpec = in.PodSpec
	out.ContainerConcurrency = v1beta1.RevisionContainerConcurrencyType(in.ContainerConcurrency)
	out.TimeoutSeconds = in.TimeoutSeconds

	// v1alpha1 fields
	out.DeprecatedGeneration = in.DeprecatedGeneration
	out.DeprecatedServingState = DeprecatedRevisionServingStateType(in.DeprecatedServingState)
	out.DeprecatedConcurrencyModel = DeprecatedRevisionRequestConcurrencyModelType(in.DeprecatedConcurrencyModel)
	out.DeprecatedBuildName = in.DeprecatedBuildName
	out.DeprecatedBuildRef = in.DeprecatedBuildRef
	out.DeprecatedContainer = in.DeprecatedContainer
	return nil
}

func Convert_v1alpha1_RevisionSpec_To_serving_RevisionSpec(in *RevisionSpec, out *serving.RevisionSpec, s conversion.Scope) error {
	// v1beta1 fields
	out.PodSpec = in.PodSpec
	out.ContainerConcurrency = serving.RevisionContainerConcurrencyType(in.ContainerConcurrency)
	out.TimeoutSeconds = in.TimeoutSeconds

	// v1alpha1 fields
	out.DeprecatedGeneration = in.DeprecatedGeneration
	out.DeprecatedServingState = serving.DeprecatedRevisionServingStateType(in.DeprecatedServingState)
	out.DeprecatedConcurrencyModel = serving.DeprecatedRevisionRequestConcurrencyModelType(in.DeprecatedConcurrencyModel)
	out.DeprecatedBuildName = in.DeprecatedBuildName
	out.DeprecatedBuildRef = in.DeprecatedBuildRef
	out.DeprecatedContainer = in.DeprecatedContainer
	return nil
}
