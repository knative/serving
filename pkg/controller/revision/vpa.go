package revision

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
)

func MakeVpa(rev *v1alpha1.Revision) *vpa.VerticalPodAutoscaler {
	return &vpa.VerticalPodAutoscaler{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetRevisionVpaName(rev),
			Namespace:   rev.Namespace,
			Labels:      MakeElaResourceLabels(rev),
			Annotations: MakeElaResourceAnnotations(rev),
		},
		Spec: vpa.VerticalPodAutoscalerSpec{
			Selector: &meta_v1.LabelSelector{
				MatchLabels: MakeElaResourceLabels(rev),
			},
			UpdatePolicy: vpa.PodUpdatePolicy{
				UpdateMode: vpa.UpdateModeAuto,
			},
			ResourcePolicy: vpa.PodResourcePolicy{
				ContainerPolicies: []vpa.ContainerResourcePolicy{
					vpa.ContainerResourcePolicy{
						Name: userContainerName,
						Mode: vpa.ContainerScalingModeOn,
						MaxAllowed: corev1.ResourceList{
							corev1.ResourceName("cpu"):    resource.MustParse(userContainerMaxCPU),
							corev1.ResourceName("memory"): resource.MustParse(userContainerMaxMemory),
						},
					},
					vpa.ContainerResourcePolicy{
						Name: queueContainerName,
						Mode: vpa.ContainerScalingModeOn,
						MaxAllowed: corev1.ResourceList{
							corev1.ResourceName("cpu"):    resource.MustParse(queueContainerMaxCPU),
							corev1.ResourceName("memory"): resource.MustParse(queueContainerMaxMemory),
						},
					},
					vpa.ContainerResourcePolicy{
						Name: fluentdContainerName,
						Mode: vpa.ContainerScalingModeOn,
						MaxAllowed: corev1.ResourceList{
							corev1.ResourceName("cpu"):    resource.MustParse(fluentdContainerMaxCPU),
							corev1.ResourceName("memory"): resource.MustParse(fluentdContainerMaxMemory),
						},
					},
					vpa.ContainerResourcePolicy{
						Name: envoyContainerName,
						Mode: vpa.ContainerScalingModeOn,
						MaxAllowed: corev1.ResourceList{
							corev1.ResourceName("cpu"):    resource.MustParse(envoyContainerMaxCPU),
							corev1.ResourceName("memory"): resource.MustParse(envoyContainerMaxMemory),
						},
					},
				},
			},
		},
	}
}
