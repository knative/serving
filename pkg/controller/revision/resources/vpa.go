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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
)

var (
	vpaUpdatePolicy = vpa.PodUpdatePolicy{
		UpdateMode: vpa.UpdateModeAuto,
	}

	vpaResourcePolicy = vpa.PodResourcePolicy{
		ContainerPolicies: []vpa.ContainerResourcePolicy{{
			Name: UserContainerName,
			Mode: vpa.ContainerScalingModeOn,
			MaxAllowed: corev1.ResourceList{
				corev1.ResourceCPU:    UserContainerMaxCPU,
				corev1.ResourceMemory: UserContainerMaxMemory,
			},
		}, {
			Name: QueueContainerName,
			Mode: vpa.ContainerScalingModeOn,
			MaxAllowed: corev1.ResourceList{
				corev1.ResourceCPU:    QueueContainerMaxCPU,
				corev1.ResourceMemory: QueueContainerMaxMemory,
			},
		}, {
			Name: FluentdContainerName,
			Mode: vpa.ContainerScalingModeOn,
			MaxAllowed: corev1.ResourceList{
				corev1.ResourceCPU:    FluentdContainerMaxCPU,
				corev1.ResourceMemory: FluentdContainerMaxMemory,
			},
		}, {
			Name: EnvoyContainerName,
			Mode: vpa.ContainerScalingModeOn,
			MaxAllowed: corev1.ResourceList{
				corev1.ResourceCPU:    EnvoyContainerMaxCPU,
				corev1.ResourceMemory: EnvoyContainerMaxMemory,
			},
		}},
	}
)

func MakeVPA(rev *v1alpha1.Revision) *vpa.VerticalPodAutoscaler {
	return &vpa.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            controller.GetRevisionVPAName(rev),
			Namespace:       controller.GetServingNamespaceName(rev.Namespace),
			Labels:          MakeLabels(rev),
			Annotations:     MakeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
		},
		Spec: vpa.VerticalPodAutoscalerSpec{
			Selector:       MakeSelector(rev),
			UpdatePolicy:   vpaUpdatePolicy,
			ResourcePolicy: vpaResourcePolicy,
		},
	}
}
