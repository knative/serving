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
	"knative.dev/pkg/ptr"

	"knative.dev/serving/pkg/apis/config"
)

func TestConfigurationDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Configuration
		want *Configuration
	}{{
		name: "empty",
		in:   &Configuration{},
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
	}, {
		name: "run latest",
		in: &Configuration{
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
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:           config.DefaultUserContainerName,
								Image:          "busybox",
								Resources:      defaultResources,
								ReadinessProbe: defaultProbe,
							}},
						},
						TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
	}, {
		name: "run latest with some default overrides",
		in: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds:       ptr.Int64(60),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				Template: RevisionTemplateSpec{
					Spec: RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:           config.DefaultUserContainerName,
								Image:          "busybox",
								Resources:      defaultResources,
								ReadinessProbe: defaultProbe,
							}},
						},
						TimeoutSeconds:       ptr.Int64(60),
						ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			got.SetDefaults(context.Background())
			if !cmp.Equal(got, test.want, ignoreUnexportedResources) {
				t.Errorf("SetDefaults (-want, +got) = %v",
					cmp.Diff(test.want, got, ignoreUnexportedResources))
			}
		})
	}
}
