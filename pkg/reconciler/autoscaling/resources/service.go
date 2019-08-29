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

package resources

import (
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/autoscaling"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	sv1a1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/resources"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeMetricsService constructs a service that can be scraped for metrics.
func MakeMetricsService(pa *pav1alpha1.PodAutoscaler, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmeta.ChildName(pa.Name, "-metrics"),
			Namespace: pa.Namespace,
			Labels: resources.UnionMaps(pa.GetLabels(), map[string]string{
				autoscaling.KPALabelKey:   pa.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypeMetrics),
			}),
			Annotations:     resources.CopyMap(pa.GetAnnotations()),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(pa)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       sv1a1.ServiceQueueMetricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       networking.AutoscalingQueueMetricsPort,
				TargetPort: intstr.FromString(sv1a1.AutoscalingQueueMetricsPortName),
			}, {
				Name:       sv1a1.UserQueueMetricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       networking.UserQueueMetricsPort,
				TargetPort: intstr.FromString(sv1a1.UserQueueMetricsPortName),
			}},
			Selector: selector,
		},
	}
}
