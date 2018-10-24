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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MakeHPA(kpa *v1alpha1.PodAutoscaler) *autoscalingv1.HorizontalPodAutoscaler {
	min, max := kpa.ScaleBounds()
	max = 100 // DO NOT SUBMIT
	hpa := &autoscalingv1.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kpa.Name,
			Namespace:       kpa.Namespace,
			Labels:          kpa.Labels,
			Annotations:     kpa.Annotations,
			OwnerReferences: kpa.OwnerReferences,
		},
		Spec: autoscalingv1.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: kpa.Spec.ScaleTargetRef,
		},
	}
	hpa.Spec.MaxReplicas = max
	if min > 0 {
		hpa.Spec.MinReplicas = &min
	}
	return hpa
}
