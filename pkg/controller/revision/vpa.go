package revision

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
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
			Labels:          MakeServingResourceLabels(rev),
			Annotations:     MakeServingResourceAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewRevisionControllerRef(rev)},
		},
		Spec: vpa.VerticalPodAutoscalerSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: MakeServingResourceLabels(rev),
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
