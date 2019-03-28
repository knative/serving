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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	logtesting "github.com/knative/pkg/logging/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/config"
)

func TestRevisionDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Revision
		want *Revision
		wc   func(context.Context) context.Context
	}{{
		name: "empty",
		in:   &Revision{},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: 0,
				TimeoutSeconds:       config.DefaultRevisionTimeoutSeconds,
				Container: corev1.Container{
					Resources: defaultResources,
				},
			},
		},
	}, {
		name: "with context",
		in:   &Revision{},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logtesting.TestLogger(t))
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"revision-timeout-seconds": "123",
				},
			})

			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: 0,
				TimeoutSeconds:       123,
				Container: corev1.Container{
					Resources: defaultResources,
				},
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
					Resources: defaultResources,
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
				Container: corev1.Container{
					Resources: defaultResources,
				},
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
				TimeoutSeconds:       config.DefaultRevisionTimeoutSeconds,
				Container: corev1.Container{
					Resources: defaultResources,
				},
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
				TimeoutSeconds:             config.DefaultRevisionTimeoutSeconds,
				Container: corev1.Container{
					Resources: defaultResources,
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got.SetDefaults(ctx)
			if diff := cmp.Diff(test.want, got, ignoreUnexportedResources); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}
