/*
Copyright 2017 The Kubernetes Authors.

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

package revision

import (
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"

	"github.com/google/elafros/pkg/controller/util"

	apiv1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeElaPodSpec creates a pod spec.
func MakeElaPodSpec(u *v1alpha1.Revision) *apiv1.PodSpec {
	name := u.Name
	serviceID := u.Spec.Service
	nginxConfigMapName := name + "-" + serviceID + "-proxy-configmap"

	elaContainer := apiv1.Container{
		Name:  elaContainerName,
		Image: u.Spec.ContainerSpec.Image,
		Resources: apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceName("cpu"): resource.MustParse("25m"),
			},
		},
		Ports: []apiv1.ContainerPort{{
			Name:          elaPortName,
			ContainerPort: int32(elaPort),
		}},
		VolumeMounts: []apiv1.VolumeMount{
			{
				MountPath: elaContainerLogVolumeMountPath,
				Name:      elaContainerLogVolumeName,
			},
			{
				MountPath: "/tmp/health-checks",
				Name:      "health-checks",
			},
		},
		Env: u.Spec.Env,
	}

	elaContainerLogVolume := apiv1.Volume{
		Name: elaContainerLogVolumeName,
		VolumeSource: apiv1.VolumeSource{
			EmptyDir: &apiv1.EmptyDirVolumeSource{},
		},
	}

	nginxContainer := apiv1.Container{
		Name:  nginxContainerName,
		Image: nginxSidecarImage,
		Resources: apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceName("cpu"): resource.MustParse("25m"),
			},
		},
		Ports: []apiv1.ContainerPort{
			// TOOD: HTTPS connections from the Cloud LB require
			// certs. Right now, the static nginx.conf file has
			// been modified to only allow HTTP connections.
			{
				Name:          nginxHttpPortName,
				ContainerPort: int32(nginxHttpPort),
			},
		},
		Env: []apiv1.EnvVar{
			{
				Name:  "CONF_FILE",
				Value: nginxConfigMountPath + "/nginx.conf",
			},
		},
		VolumeMounts: []apiv1.VolumeMount{
			{
				MountPath: nginxConfigMountPath,
				Name:      nginxConfigMapName,
				ReadOnly:  true,
			},
			{
				MountPath: nginxLogVolumeMountPath,
				Name:      nginxLogVolumeName,
			},
			{
				MountPath: "/tmp/health-checks",
				Name:      "health-checks",
			},
		},
		Lifecycle: &apiv1.Lifecycle{
			PostStart: &apiv1.Handler{
				Exec: &apiv1.ExecAction{
					Command: []string{
						"rm", "/tmp/health-checks/app_lameducked",
					},
				},
			},
		},
		ReadinessProbe: &apiv1.Probe{
			Handler: apiv1.Handler{
				Exec: &apiv1.ExecAction{
					Command: []string{
						"/bin/sh", "-c",
						"test ! -f /tmp/health-checks/app_lameducked",
					},
				},
			},
		},
	}

	nginxConfigVolume := apiv1.Volume{
		Name: nginxConfigMapName,
		VolumeSource: apiv1.VolumeSource{
			ConfigMap: &apiv1.ConfigMapVolumeSource{
				LocalObjectReference: apiv1.LocalObjectReference{
					Name: nginxConfigMapName,
				},
			},
		},
	}

	nginxLogVolume := apiv1.Volume{
		Name: nginxLogVolumeName,
		VolumeSource: apiv1.VolumeSource{
			EmptyDir: &apiv1.EmptyDirVolumeSource{},
		},
	}

	fluentdContainer := apiv1.Container{
		Name:  fluentdContainerName,
		Image: fluentdSidecarImage,
		Resources: apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceName("cpu"): resource.MustParse("25m"),
			},
		},
		VolumeMounts: []apiv1.VolumeMount{
			{
				MountPath: nginxLogVolumeMountPath,
				Name:      nginxLogVolumeName,
				//ReadOnly:  true,
			},
			{
				MountPath: elaContainerLogVolumeMountPath,
				Name:      elaContainerLogVolumeName,
				//ReadOnly:  true,
			},
		},
	}

	initContainer := apiv1.Container{
		Name:  "health-check-seeder",
		Image: "gcr.io/google_appengine/base",
		Resources: apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceName("cpu"): resource.MustParse("25m"),
			},
		},
		Command: []string{
			"touch", "/tmp/health-checks/app_lameducked",
			"/tmp/health-checks/lameducked",
		},
		VolumeMounts: []apiv1.VolumeMount{
			{
				MountPath: "/tmp/health-checks",
				Name:      "health-checks",
			},
		},
	}

	healthCheckVolume := apiv1.Volume{
		Name: "health-checks",
		VolumeSource: apiv1.VolumeSource{
			EmptyDir: &apiv1.EmptyDirVolumeSource{},
		},
	}

	return &apiv1.PodSpec{
		Volumes:    []apiv1.Volume{healthCheckVolume, elaContainerLogVolume, nginxConfigVolume, nginxLogVolume},
		Containers: []apiv1.Container{elaContainer, nginxContainer, fluentdContainer}, InitContainers: []apiv1.Container{initContainer},
	}
}

// MakeElaDeploymentLabels constructs the labels we will apply to K8s resources.
func MakeElaDeploymentLabels(u *v1alpha1.Revision) map[string]string {
	name := u.Name
	serviceID := u.Spec.Service

	return map[string]string{
		elaServiceLabel: serviceID,
		elaVersionLabel: name,
	}
}

// MakeElaDeployment creates a deployment.
func MakeElaDeployment(u *v1alpha1.Revision, namespace string) *v1beta1.Deployment {
	rollingUpdateConfig := v1beta1.RollingUpdateDeployment{
		MaxUnavailable: &elaPodMaxUnavailable,
		MaxSurge:       &elaPodMaxSurge,
	}

	return &v1beta1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      util.GetRevisionDeploymentName(u),
			Namespace: namespace,
			Labels:    MakeElaDeploymentLabels(u),
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &elaPodReplicaCount,
			Strategy: v1beta1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels: MakeElaDeploymentLabels(u),
				},
			},
		},
	}
}
