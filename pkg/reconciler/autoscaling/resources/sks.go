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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	nv1a1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/reconciler/autoscaling/resources/names"
)

// MakeSKS makes an SKS resource from the PA and operation mode.
func MakeSKS(pa *autoscalingv1alpha1.PodAutoscaler, mode nv1a1.ServerlessServiceOperationMode, numActivators int32) *nv1a1.ServerlessService {
	return &nv1a1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SKS(pa.Name),
			Namespace: pa.Namespace,
			Labels:    kmeta.CopyMap(pa.GetLabels()),
			Annotations: kmeta.FilterMap(pa.GetAnnotations(), func(s string) bool {
				return s == autoscaling.MetricAnnotationKey
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(pa)},
		},
		Spec: nv1a1.ServerlessServiceSpec{
			Mode:          mode,
			ObjectRef:     pa.Spec.ScaleTargetRef,
			ProtocolType:  pa.Spec.ProtocolType,
			NumActivators: numActivators,
		},
	}
}
