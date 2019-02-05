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

package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

func TestRevisionDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Revision
		want *Revision
	}{{
		name: "empty",
		in:   &Revision{},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: 0,
				TimeoutSeconds:       defaultTimeoutSeconds,
			},
		},
	}, {
		name: "readonly volumes",
		in: &Revision{
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "foo",
					VolumeMounts: []corev1.VolumeMount{{
						Name: "bar",
					}},
				},
				ContainerConcurrency: 1,
				TimeoutSeconds:       99,
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "foo",
					VolumeMounts: []corev1.VolumeMount{{
						Name:     "bar",
						ReadOnly: true,
					}},
				},
				ContainerConcurrency: 1,
				TimeoutSeconds:       99,
			},
		},
	}, {
		name: "no overwrite",
		in: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       99,
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       99,
			},
		},
	}, {
		name: "partially initialized",
		in: &Revision{
			Spec: RevisionSpec{},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: 0,
				TimeoutSeconds:       defaultTimeoutSeconds,
			},
		},
	}, {
		name: "fall back to concurrency model",
		in: &Revision{
			Spec: RevisionSpec{
				DeprecatedConcurrencyModel: "Single",
				ContainerConcurrency:       0, // unspecified
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				DeprecatedConcurrencyModel: "Single",
				ContainerConcurrency:       1,
				TimeoutSeconds:             defaultTimeoutSeconds,
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			got.SetDefaults()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}
