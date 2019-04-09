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

package v1beta1

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
)

func TestConfigurationValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *Configuration
		want *apis.FieldError
	}{{
		name: "valid",
		r: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid container concurrency",
		r: &Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						ContainerConcurrency: -10,
					},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(
			-10, 0, RevisionContainerConcurrencyMax,
			"spec.template.spec.containerConcurrency"),
	}}

	// TODO(dangerd): PodSpec validation failures.
	// TODO(mattmoor): BYO Revision name.

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
