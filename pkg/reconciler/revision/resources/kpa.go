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

	"github.com/knative/pkg/kmeta"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/revision/resources/names"
	"github.com/knative/serving/pkg/resources"
)

// MakeKPA makes a Knative Pod Autoscaler resource from a revision.
func MakeKPA(rev *v1alpha1.Revision) *kpa.PodAutoscaler {
	return &kpa.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.KPA(rev),
			Namespace: rev.Namespace,
			Labels:    makeLabels(rev),
			Annotations: resources.FilterMap(rev.GetAnnotations(), func(k string) bool {
				// Ignore last pinned annotation.
				return k == serving.RevisionLastPinnedAnnotationKey
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
		},
		Spec: kpa.PodAutoscalerSpec{
			ContainerConcurrency: rev.Spec.ContainerConcurrency,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       names.Deployment(rev),
			},
			ProtocolType: rev.GetProtocol(),
		},
	}
}
