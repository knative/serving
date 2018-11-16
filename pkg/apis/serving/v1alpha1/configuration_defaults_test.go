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
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						TimeoutSeconds: &metav1.Duration{
							Duration: 60 * time.Second,
						},
					},
				},
			},
		},
	}, {
		name: "no overwrite values",
		in: &Configuration{
			Spec: ConfigurationSpec{
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ContainerConcurrency: 1,
						TimeoutSeconds: &metav1.Duration{
							Duration: 99 * time.Second,
						},
					},
				},
			},
		},
		want: &Configuration{
			Spec: ConfigurationSpec{
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ContainerConcurrency: 1,
						TimeoutSeconds: &metav1.Duration{
							Duration: 99 * time.Second,
						},
					},
				},
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
