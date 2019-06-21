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
	"math"

	"github.com/knative/pkg/kmeta"
	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	aresources "github.com/knative/serving/pkg/reconciler/autoscaling/resources"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeHPA creates an HPA resource from a PA resource.
func MakeHPA(pa *v1alpha1.PodAutoscaler, config *autoscaler.Config) *autoscalingv2beta1.HorizontalPodAutoscaler {
	min, max := pa.ScaleBounds()
	if max == 0 {
		max = math.MaxInt32 // default to no limit
	}
	hpa := &autoscalingv2beta1.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pa.Name,
			Namespace:       pa.Namespace,
			Labels:          pa.Labels,
			Annotations:     pa.Annotations,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(pa)},
		},
		Spec: autoscalingv2beta1.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2beta1.CrossVersionObjectReference{
				APIVersion: pa.Spec.ScaleTargetRef.APIVersion,
				Kind:       pa.Spec.ScaleTargetRef.Kind,
				Name:       pa.Spec.ScaleTargetRef.Name,
			},
		},
	}
	hpa.Spec.MaxReplicas = max
	if min > 0 {
		hpa.Spec.MinReplicas = &min
	}

	switch pa.Metric() {
	case autoscaling.CPU:
		if target, ok := pa.Target(); ok {
			hpa.Spec.Metrics = []autoscalingv2beta1.MetricSpec{{
				Type: autoscalingv2beta1.ResourceMetricSourceType,
				Resource: &autoscalingv2beta1.ResourceMetricSource{
					Name:                     corev1.ResourceCPU,
					TargetAverageUtilization: ptr.Int32(int32(math.Ceil(target))),
				},
			}}
		}
	case autoscaling.Concurrency:
		target := int64(math.Ceil(aresources.ResolveConcurrency(pa, config)))
		hpa.Spec.Metrics = []autoscalingv2beta1.MetricSpec{{
			Type: autoscalingv2beta1.ObjectMetricSourceType,
			Object: &autoscalingv2beta1.ObjectMetricSource{
				Target: autoscalingv2beta1.CrossVersionObjectReference{
					APIVersion: servingv1alpha1.SchemeGroupVersion.String(),
					Kind:       "revision",
					Name:       pa.Name,
				},
				MetricName:   autoscaling.Concurrency,
				AverageValue: resource.NewQuantity(target, resource.DecimalSI),
				TargetValue:  *resource.NewQuantity(target, resource.DecimalSI),
			},
		}}
	}
	return hpa
}
