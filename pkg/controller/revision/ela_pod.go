/*
Copyright 2018 Google LLC

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
	appsv1 "k8s.io/api/apps/v1"
	//v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// Each Elafros pod gets 1 cpu.
	elaContainerCPU     = "400m"
	queueContainerCPU   = "25m"
	fluentdContainerCPU = "75m"

	fluentdConfigMapVolumeName = "configmap"
	varLogVolumeName           = "varlog"
)

func hasHttpPath(p *corev1.Probe) bool {
	if p == nil {
		return false
	}
	if p.Handler.HTTPGet == nil {
		return false
	}
	return p.Handler.HTTPGet.Path != ""
}

// MakeElaPodSpec creates a pod spec.
func MakeElaPodSpec(rev *v1alpha1.Revision, fluentdSidecarImage, queueSidecarImage string) *corev1.PodSpec {
	varLogVolume := corev1.Volume{
		Name: varLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	fluentdConfigMapVolume := corev1.Volume{
		Name: fluentdConfigMapVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "fluentd-varlog-config",
				},
			},
		},
	}

	elaContainer := rev.Spec.Container.DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the validations in pkg/webhook.validateContainer.
	elaContainer.Name = elaContainerName
	elaContainer.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName("cpu"): resource.MustParse(elaContainerCPU),
		},
	}
	elaContainer.Ports = []corev1.ContainerPort{{
		Name:          elaPortName,
		ContainerPort: int32(elaPort),
	}}
	elaContainer.VolumeMounts = append(
		elaContainer.VolumeMounts,
		corev1.VolumeMount{
			Name:      varLogVolumeName,
			MountPath: "/var/log",
		},
	)
	// Add our own PreStop hook here, which should do two things:
	// - make the container fails the next readinessCheck to avoid
	//   having more traffic, and
	// - add a small delay so that the container stays alive a little
	//   bit longer in case stoppage of traffic is not effective
	//   immediately.
	//
	// TODO(tcnghia): Fail validation webhook when users specify their
	// own lifecycle hook.
	elaContainer.Lifecycle = &corev1.Lifecycle{
		PreStop: &corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(RequestQueueAdminPort),
				Path: RequestQueueQuitPath,
			},
		},
	}
	// If the client provided a readiness check endpoint, we should
	// fill in the port for them so that requests also go through
	// queue proxy for a better health checking logic.
	//
	// TODO(tcnghia): Fail validation webhook when users specify their
	// own port in readiness checks.
	if hasHttpPath(elaContainer.ReadinessProbe) {
		elaContainer.ReadinessProbe.Handler.HTTPGet.Port = intstr.FromInt(RequestQueuePort)
	}

	fluentdContainer := corev1.Container{
		Name:  fluentdContainerName,
		Image: fluentdSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(fluentdContainerCPU),
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "FLUENTD_ARGS",
				Value: "--no-supervisor -q",
			},
			{
				Name:  "ELA_CONTAINER_NAME",
				Value: elaContainerName,
			},
			{
				Name:  "ELA_CONFIGURATION",
				Value: controller.LookupOwningConfigurationName(rev.OwnerReferences),
			},
			{
				Name:  "ELA_REVISION",
				Value: rev.Name,
			},
			{
				Name:  "ELA_NAMESPACE",
				Value: rev.Namespace,
			},
			{
				Name: "ELA_POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      varLogVolumeName,
				MountPath: "/var/log/revisions",
			},
			{
				Name:      fluentdConfigMapVolumeName,
				MountPath: "/etc/fluent/config.d",
			},
		},
	}
	queueContainer := corev1.Container{
		Name:  queueContainerName,
		Image: queueSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(queueContainerCPU),
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          RequestQueuePortName,
				ContainerPort: int32(RequestQueuePort),
			},
			// Provides health checks and lifecycle hooks.
			{
				Name:          RequestQueueAdminPortName,
				ContainerPort: int32(RequestQueueAdminPort),
			},
		},
		// This handler (1) marks the service as not ready and (2)
		// adds a small delay before the container is killed.
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(RequestQueueAdminPort),
					Path: RequestQueueQuitPath,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(RequestQueueAdminPort),
					Path: RequestQueueHealthPath,
				},
			},
			// We want to mark the service as not ready as soon as the
			// PreStop handler is called, so we need to check a little
			// bit more often than the default.  It is a small
			// sacrifice for a low rate of 503s.
			PeriodSeconds: 1,
		},
		Args: []string{
			"-logtostderr=true",
			"-stderrthreshold=INFO",
		},
		Env: []corev1.EnvVar{
			{
				Name:  "ELA_NAMESPACE",
				Value: rev.Namespace,
			},
			{
				Name:  "ELA_REVISION",
				Value: rev.Name,
			},
			{
				Name:  "ELA_AUTOSCALER",
				Value: controller.GetRevisionAutoscalerName(rev),
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
	return &corev1.PodSpec{
		Containers:         []corev1.Container{*elaContainer, fluentdContainer, queueContainer},
		Volumes:            []corev1.Volume{varLogVolume, fluentdConfigMapVolume},
		ServiceAccountName: rev.Spec.ServiceAccountName,
	}
}

// MakeElaDeployment creates a deployment.
func MakeElaDeployment(u *v1alpha1.Revision, namespace string) *appsv1.Deployment {
	rollingUpdateConfig := appsv1.RollingUpdateDeployment{
		MaxUnavailable: &elaPodMaxUnavailable,
		MaxSurge:       &elaPodMaxSurge,
	}

	podTemplateAnnotations := MakeElaResourceAnnotations(u)
	podTemplateAnnotations[sidecarIstioInjectAnnotation] = "true"

	return &appsv1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetRevisionDeploymentName(u),
			Namespace:   namespace,
			Labels:      MakeElaResourceLabels(u),
			Annotations: MakeElaResourceAnnotations(u),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &elaPodReplicaCount,
			Strategy: appsv1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels:      MakeElaResourceLabels(u),
					Annotations: podTemplateAnnotations,
				},
			},
		},
	}
}
