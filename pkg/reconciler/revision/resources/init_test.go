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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMakeInitInternalVolume(t *testing.T) {
	tests := []struct {
		name string
		want *corev1.Volume
	}{{
		name: "happy path",
		want: &corev1.Volume{
			Name: internalVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makeInitInternalVolume()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("makeInitInternalVolume (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeInitPodInfoVolume(t *testing.T) {
	tests := []struct {
		name string
		want *corev1.Volume
	}{{
		name: "happy path",
		want: &corev1.Volume{
			Name: podInfoVolumeName,
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path: podInfoPodName,
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.name",
							},
						},
						{
							Path: podInfoNamespace,
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.namespace",
							},
						},
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makeInitPodInfoVolume()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("makeInitPodInfoVolume (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeInitContainer(t *testing.T) {
	tests := []struct {
		name string
		want *corev1.Container
	}{{
		name: "happy path",
		want: &corev1.Container{
			// These are effectively constants
			Name:         InitContainerName,
			Image:        InitContainerImage,
			Args:         initArgs,
			VolumeMounts: initVolumeMounts,
		},
	},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makeInitContainer()
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeInitContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestInitArgs(t *testing.T) {
	want := []string{
		"/bin/sh",
		"-c",
		"ln -s ../knative-var-log \"/var/knative-internal/$(cat /etc/pod-info/namespace)_$(cat /etc/pod-info/podname)_user-container\"",
	}
	if len(want) != len(initArgs) {
		t.Errorf("length mismatch. want: %d, got: %d", len(want), len(initArgs))
	}
	for i, _ := range initArgs {
		if initArgs[i] != want[i] {
			t.Errorf("argument mismatch at index %d. want: %s, got: %s", i, want[i], initArgs[i])
		}
	}
}
