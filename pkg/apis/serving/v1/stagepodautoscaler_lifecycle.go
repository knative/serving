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

package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"

	"knative.dev/pkg/apis"
)

var podCondSet = apis.NewLivingConditionSet()

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*StagePodAutoscaler) GetConditionSet() apis.ConditionSet {
	return podCondSet
}

// GetGroupVersionKind returns the GVK for the PodAutoscaler.
func (pa *StagePodAutoscaler) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("StagePodAutoscaler")
}

func (pa *StagePodAutoscaler) ScaleBounds(asConfig *autoscalerconfig.Config) (*int32, *int32) {
	return pa.Spec.MinScale, pa.Spec.MaxScale
}
