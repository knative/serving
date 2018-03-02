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
	"strconv"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Each Elafros pod gets 1 cpu.
	elaContainerCpu     = "400m"
	queueContainerCpu   = "25m"
	nginxContainerCpu   = "25m"
	fluentdContainerCpu = "100m"
)

// MakeElaPodSpec creates a pod spec.
func MakeElaPodSpec(u *v1alpha1.Revision) *corev1.PodSpec {
	name := u.Name
	serviceID := u.Spec.Service
	nginxConfigMapName := name + "-" + serviceID + "-proxy-configmap"

	elaContainer := u.Spec.ContainerSpec.DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the validations in pkg/webhook.validateContainerSpec.
	elaContainer.Name = elaContainerName
	elaContainer.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName("cpu"): resource.MustParse(elaContainerCpu),
		},
	}
	elaContainer.Ports = []corev1.ContainerPort{{
		Name:          elaPortName,
		ContainerPort: int32(elaPort),
	}}
	elaContainer.VolumeMounts = []corev1.VolumeMount{
		{
			MountPath: elaContainerLogVolumeMountPath,
			Name:      elaContainerLogVolumeName,
		},
	}

	elaContainerLogVolume := corev1.Volume{
		Name: elaContainerLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	queueContainer := corev1.Container{
		Name:  queueContainerName,
		Image: queueSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(queueContainerCpu),
			},
		},
		Ports: []corev1.ContainerPort{
			// TOOD: HTTPS connections from the Cloud LB require
			// certs. Right now, the static nginx.conf file has
			// been modified to only allow HTTP connections.
			{
				Name:          requestQueuePortName,
				ContainerPort: int32(requestQueuePort),
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "ELA_NAMESPACE",
				Value: u.Namespace,
			},
			{
				Name:  "ELA_REVISION",
				Value: u.Name,
			},
			{
				Name:  "ELA_AUTOSCALER",
				Value: controller.GetRevisionAutoscalerName(u),
			},
			{
				Name:  "ELA_AUTOSCALER_PORT",
				Value: strconv.Itoa(autoscalerPort),
			},
			{
				Name: "ELA_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
		},
	}

	nginxContainer := corev1.Container{
		Name:  nginxContainerName,
		Image: nginxSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(nginxContainerCpu),
			},
		},
		Ports: []corev1.ContainerPort{
			// TOOD: HTTPS connections from the Cloud LB require
			// certs. Right now, the static nginx.conf file has
			// been modified to only allow HTTP connections.
			{
				Name:          nginxHTTPPortName,
				ContainerPort: int32(nginxHTTPPort),
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "CONF_FILE",
				Value: nginxConfigMountPath + "/nginx.conf",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				MountPath: nginxConfigMountPath,
				Name:      nginxConfigMapName,
				ReadOnly:  true,
			},
			{
				MountPath: nginxLogVolumeMountPath,
				Name:      nginxLogVolumeName,
			},
		},
	}

	nginxConfigVolume := corev1.Volume{
		Name: nginxConfigMapName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: nginxConfigMapName,
				},
			},
		},
	}

	nginxLogVolume := corev1.Volume{
		Name: nginxLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	fluentdContainer := corev1.Container{
		Name:  fluentdContainerName,
		Image: fluentdSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(fluentdContainerCpu),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
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

	return &corev1.PodSpec{
		Volumes:            []corev1.Volume{elaContainerLogVolume, nginxConfigVolume, nginxLogVolume},
		Containers:         []corev1.Container{*elaContainer, queueContainer, nginxContainer, fluentdContainer},
		ServiceAccountName: "ela-revision",
	}
}

// MakeElaDeploymentLabels constructs the labels we will apply to K8s resources.
func MakeElaDeploymentLabels(u *v1alpha1.Revision) map[string]string {
	name := u.Name
	serviceID := u.Spec.Service

	return map[string]string{
		routeLabel:      serviceID,
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
			Name:      controller.GetRevisionDeploymentName(u),
			Namespace: namespace,
			Labels:    MakeElaDeploymentLabels(u),
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &elaPodReplicaCount,
			Strategy: v1beta1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels: MakeElaDeploymentLabels(u),
				},
			},
		},
	}
}
