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

package autoscaling

const (
	InternalGroupName = "autoscaling.internal.knative.dev"

	GroupName = "autoscaling.knative.dev"

	// ClassAnnotationKey is the annotation for the explicit class of autoscaler
	// that a particular resource has opted into. For example,
	//   autoscaling.knative.dev/class: foo
	// This uses a different domain because unlike the resource, it is user-facing.
	ClassAnnotationKey = GroupName + "/class"

	MinScaleAnnotationKey = GroupName + "/minScale"
	MaxScaleAnnotationKey = GroupName + "/maxScale"

	// KPALabelKey is the label key attached to a K8s Service to hint to the KPA
	// which services/endpoints should trigger reconciles.
	KPALabelKey = GroupName + "/kpa"
)
