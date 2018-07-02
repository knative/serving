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

package revision

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/revision/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
)

func MakeVPA(rev *v1alpha1.Revision) *vpa.VerticalPodAutoscaler {
	return &vpa.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            controller.GetRevisionVPAName(rev),
			Namespace:       controller.GetServingNamespaceName(rev.Namespace),
			Labels:          resources.MakeLabels(rev),
			Annotations:     resources.MakeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
		},
		Spec: vpa.VerticalPodAutoscalerSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: resources.MakeLabels(rev),
			},
			UpdatePolicy: vpa.PodUpdatePolicy{
				UpdateMode: vpa.UpdateModeAuto,
			},
			ResourcePolicy: vpa.PodResourcePolicy{
				ContainerPolicies: []vpa.ContainerResourcePolicy{{
					Name: userContainerName,
					Mode: vpa.ContainerScalingModeOn,
					MaxAllowed: corev1.ResourceList{
						corev1.ResourceName("cpu"):    resource.MustParse(userContainerMaxCPU),
						corev1.ResourceName("memory"): resource.MustParse(userContainerMaxMemory),
					},
				}, {
					Name: queueContainerName,
					Mode: vpa.ContainerScalingModeOn,
					MaxAllowed: corev1.ResourceList{
						corev1.ResourceName("cpu"):    resource.MustParse(queueContainerMaxCPU),
						corev1.ResourceName("memory"): resource.MustParse(queueContainerMaxMemory),
					},
				}, {
					Name: fluentdContainerName,
					Mode: vpa.ContainerScalingModeOn,
					MaxAllowed: corev1.ResourceList{
						corev1.ResourceName("cpu"):    resource.MustParse(fluentdContainerMaxCPU),
						corev1.ResourceName("memory"): resource.MustParse(fluentdContainerMaxMemory),
					},
				}, {
					Name: envoyContainerName,
					Mode: vpa.ContainerScalingModeOn,
					MaxAllowed: corev1.ResourceList{
						corev1.ResourceName("cpu"):    resource.MustParse(envoyContainerMaxCPU),
						corev1.ResourceName("memory"): resource.MustParse(envoyContainerMaxMemory),
					},
				}},
			},
		},
	}
}
