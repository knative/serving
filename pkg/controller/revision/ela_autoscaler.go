/*
Copyright 2017 The Kubernetes Authors.

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
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/google/elafros/pkg/controller"

	autoscaling_v1 "k8s.io/api/autoscaling/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO(josephburnett): make autoscaler service instead of hpa
func MakeElaAutoscaler(u *v1alpha1.Revision, namespace string) *autoscaling_v1.HorizontalPodAutoscaler {
	name := u.Name
	serviceID := u.Spec.Service

	var min int32 = 1
	var max int32 = 10
	var targetPercentage int32 = 50

	return &autoscaling_v1.HorizontalPodAutoscaler{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      controller.GetRevisionAutoscalerName(u),
			Namespace: namespace,
			Labels: map[string]string{
				routeLabel:      serviceID,
				elaVersionLabel: name,
			},
		},
		Spec: autoscaling_v1.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscaling_v1.CrossVersionObjectReference{
				APIVersion: "extensions/v1beta1",
				Kind:       "Deployment",
				Name:       controller.GetRevisionDeploymentName(u),
			},
			MinReplicas:                    &min,
			MaxReplicas:                    int32(max),
			TargetCPUUtilizationPercentage: &targetPercentage,
		},
	}
}
