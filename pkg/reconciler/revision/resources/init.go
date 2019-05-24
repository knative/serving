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
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"
)

const (
	internalVolumeName      = "knative-internal"
	internalVolumeMountPath = "/var/knative-internal"

	podInfoVolumeName      = "pod-info"
	podInfoVolumeMountPath = "/etc/pod-info"
	podInfoPodName         = "podname"
	podInfoNamespace       = "namespace"

	cmdFmt = "ln -s ../%s \"%s/$(cat %s)_$(cat %s)_%s\""
)

var (
	initVolumeMounts = []corev1.VolumeMount{{
		Name:      internalVolumeName,
		MountPath: internalVolumeMountPath,
	}, {
		Name:      podInfoVolumeName,
		MountPath: podInfoVolumeMountPath,
	}}

	// Create a symlink to enable fluentd log collection on the host
	//   - From container POV: /var/knative-internal/{NAMESPACE}_{POD_NAME}_{USER_CONTAINER_NAME} -> ../knative-var-log
	//   - From host POV: <POD_VOLUME_DIRECTORY>/knative-internal/{NAMESPACE}_{POD_NAME}_{USER_CONTAINER_NAME} -> ../knative-var-log
	// This way, there is a folder on the host with the metadata required by fluentd for proper event enrichment (see 100-fluentd-configmap.yaml)
	initArgs = []string{
		"/bin/sh",
		"-c",
		fmt.Sprintf(
			"ln -s ../%s \"%s/$(cat %s)_$(cat %s)_%s\"",
			varLogVolumeName,
			internalVolumeMountPath,
			path.Join(podInfoVolumeMountPath, podInfoNamespace),
			path.Join(podInfoVolumeMountPath, podInfoPodName),
			UserContainerName),
	}
)

func makeInitInternalVolume() *corev1.Volume {
	return &corev1.Volume{
		Name: internalVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

func makeInitPodInfoVolume() *corev1.Volume {
	return &corev1.Volume{
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
	}
}

func makeInitContainer() *corev1.Container {
	return &corev1.Container{
		Name:         InitContainerName,
		Image:        InitContainerImage,
		Args:         initArgs,
		VolumeMounts: initVolumeMounts,
	}
}
