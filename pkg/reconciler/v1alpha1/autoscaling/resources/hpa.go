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

package resources

import (
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
)

func MakeHPA(kpa *v1alpha1.PodAutoscaler) *autoscalingv1.HorizontalPodAutoscaler {
	min, max := kpa.ScaleBounds()
	hpa := &autoscalingv1.HorizontalPodAutoscaler{
		Spec: autoscalingv1.HorizontalPodAutoscalingSpec{
			ScaleTargetRef: kpa.ScaleTargetRef,
		},
	}
	if min != 0 {
		hpa.Spec.MinReplicas = &min
	}
	if max != 0 {
		hpa.Spec.MaxReplicas = &max
	}
	return hpa
}
