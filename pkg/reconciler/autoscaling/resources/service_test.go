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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	pav1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

func TestMakeService(t *testing.T) {
	for in, want := range map[string]string{
		"with-you":                           "with-you-metrics",
		strings.Repeat("apples-on-venus", 7): kmeta.ChildName(strings.Repeat("apples-on-venus", 7), "-metrics"),
	} {
		t.Run(in, func(t *testing.T) {
			pa := &pav1a1.PodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "here",
					Name:      in,
					UID:       "2006",
					// Those labels are propagated from the Revision->PA.
					Labels: map[string]string{
						serving.RevisionLabelKey: in,
						serving.RevisionUID:      "2009",
					},
					Annotations: map[string]string{
						"a": "b",
					},
				},
				Spec: pav1a1.PodAutoscalerSpec{
					ScaleTargetRef: corev1.ObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "with-you",
					},
				},
			}
			selector := map[string]string{"cant": "stop"}
			wantS := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "here",
					Name:      want,
					Labels: map[string]string{
						// Those should be propagated.
						serving.RevisionLabelKey:  in,
						serving.RevisionUID:       "2009",
						autoscaling.KPALabelKey:   in,
						networking.ServiceTypeKey: string(networking.ServiceTypeMetrics),
					},
					Annotations: map[string]string{
						"a": "b",
					},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         pav1a1.SchemeGroupVersion.String(),
						Kind:               "PodAutoscaler",
						Name:               in,
						UID:                "2006",
						Controller:         ptr.Bool(true),
						BlockOwnerDeletion: ptr.Bool(true),
					}},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{
						Name:       "metrics",
						Protocol:   corev1.ProtocolTCP,
						Port:       9090,
						TargetPort: intstr.FromString("queue-metrics"),
					}, {
						Name:       v1alpha1.UserQueueMetricsPortName,
						Protocol:   corev1.ProtocolTCP,
						Port:       networking.UserQueueMetricsPort,
						TargetPort: intstr.FromString(v1alpha1.UserQueueMetricsPortName),
					}},
					Selector: selector,
				},
			}
			got := MakeMetricsService(pa, selector)
			if diff := cmp.Diff(wantS, got); diff != "" {
				t.Errorf("Metrics K8s Service mismatch (-want, +got) = %v", diff)
			}
		})
	}
}
