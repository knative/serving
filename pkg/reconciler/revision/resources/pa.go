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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/kmeta"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"
)

// MakePA makes a Knative Pod Autoscaler resource from a revision.
func MakePA(rev *v1.Revision) *autoscalingv1alpha1.PodAutoscaler {
	return &autoscalingv1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.PA(rev),
			Namespace:       rev.Namespace,
			Labels:          makeLabels(rev),
			Annotations:     makeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
		},
		Spec: autoscalingv1alpha1.PodAutoscalerSpec{
			ContainerConcurrency: rev.Spec.GetContainerConcurrency(),
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       names.Deployment(rev),
			},
			ProtocolType: rev.GetProtocol(),
			Reachability: func() autoscalingv1alpha1.ReachabilityType {
				// If the Revision has failed to become Ready, then mark the PodAutoscaler as unreachable.
				if rev.Status.GetCondition(v1.RevisionConditionReady).IsFalse() {
					// Make sure that we don't do this when a newly failing revision is
					// marked reachable by outside forces.
					if !rev.IsReachable() {
						return autoscalingv1alpha1.ReachabilityUnreachable
					}
				}

				// We don't know the reachability if the revision has just been created
				// or it is activating.
				if rev.Status.GetCondition(v1.RevisionConditionActive).IsUnknown() {
					return autoscalingv1alpha1.ReachabilityUnknown
				}

				if rev.IsReachable() {
					return autoscalingv1alpha1.ReachabilityReachable
				}
				return autoscalingv1alpha1.ReachabilityUnreachable
			}(),
		},
	}
}
