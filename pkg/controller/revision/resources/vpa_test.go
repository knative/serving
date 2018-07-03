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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestMakeVPA(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		want *vpa.VerticalPodAutoscaler
	}{{
		name: "name is bar",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
		},
		want: &vpa.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-vpa",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					appLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: vpa.VerticalPodAutoscalerSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						appLabelKey:              "bar",
					},
				},
				UpdatePolicy:   vpaUpdatePolicy,
				ResourcePolicy: vpaResourcePolicy,
			},
		},
	}, {
		name: "name is baz",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "baz",
				UID:       "4321",
			},
		},
		want: &vpa.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "blah",
				Name:      "baz-vpa",
				Labels: map[string]string{
					serving.RevisionLabelKey: "baz",
					serving.RevisionUID:      "4321",
					appLabelKey:              "baz",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "baz",
					UID:                "4321",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: vpa.VerticalPodAutoscalerSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionLabelKey: "baz",
						serving.RevisionUID:      "4321",
						appLabelKey:              "baz",
					},
				},
				UpdatePolicy:   vpaUpdatePolicy,
				ResourcePolicy: vpaResourcePolicy,
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeVPA(test.rev)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("MakeK8sService (-want, +got) = %v", diff)
			}
		})
	}
}
