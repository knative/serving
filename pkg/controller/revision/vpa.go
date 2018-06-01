package revision

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
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
				ContainerPolicies: []vpa.ContainerResourcePolicy{},
			},
		},
	}
}
