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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/queue"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// See https://github.com/knative/serving/pull/1124#issuecomment-397120430
	// for how CPU and memory values were calculated.

	// Each Knative Serving pod gets 500m cpu initially.
	userContainerCPU    = "400m"
	queueContainerCPU   = "25m"
	fluentdContainerCPU = "25m"
	envoyContainerCPU   = "50m"

	// Limit CPU recommendation to 2000m
	userContainerMaxCPU    = "1700m"
	queueContainerMaxCPU   = "200m"
	fluentdContainerMaxCPU = "100m"
	envoyContainerMaxCPU   = "200m"

	// Limit memory recommendation to 4G
	userContainerMaxMemory    = "3700M"
	queueContainerMaxMemory   = "100M"
	fluentdContainerMaxMemory = "100M"
	envoyContainerMaxMemory   = "100M"

	fluentdConfigMapVolumeName = "configmap"
	varLogVolumeName           = "varlog"
)

func hasHTTPPath(p *corev1.Probe) bool {
	if p == nil {
		return false
	}
	if p.Handler.HTTPGet == nil {
		return false
	}
	return p.Handler.HTTPGet.Path != ""
}

// MakeElaPodSpec creates a pod spec.
func MakeElaPodSpec(
	rev *v1alpha1.Revision,
	controllerConfig *ControllerConfig) *corev1.PodSpec {
	varLogVolume := corev1.Volume{
		Name: varLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	userContainer := rev.Spec.Container.DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the validations in pkg/webhook.validateContainer.
	userContainer.Name = userContainerName
	userContainer.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName("cpu"): resource.MustParse(userContainerCPU),
		},
	}
	userContainer.Ports = []corev1.ContainerPort{{
		Name:          userPortName,
		ContainerPort: int32(userPort),
	}}
	userContainer.VolumeMounts = append(
		userContainer.VolumeMounts,
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
	userContainer.Lifecycle = &corev1.Lifecycle{
		PreStop: &corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(queue.RequestQueueAdminPort),
				Path: queue.RequestQueueQuitPath,
			},
		},
	}
	// If the client provided a readiness check endpoint, we should
	// fill in the port for them so that requests also go through
	// queue proxy for a better health checking logic.
	//
	// TODO(tcnghia): Fail validation webhook when users specify their
	// own port in readiness checks.
	if hasHTTPPath(userContainer.ReadinessProbe) {
		userContainer.ReadinessProbe.Handler.HTTPGet.Port = intstr.FromInt(queue.RequestQueuePort)
	}

	podSpe := &corev1.PodSpec{
		Containers:         []corev1.Container{*userContainer, *MakeElaQueueContainer(rev, controllerConfig)},
		Volumes:            []corev1.Volume{varLogVolume},
		ServiceAccountName: rev.Spec.ServiceAccountName,
	}

	// Add Fluentd sidecar and its config map volume if var log collection is enabled.
	if controllerConfig.EnableVarLogCollection {
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

		fluentdContainer := corev1.Container{
			Name:  fluentdContainerName,
			Image: controllerConfig.FluentdSidecarImage,
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
					Value: userContainerName,
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

		podSpe.Containers = append(podSpe.Containers, fluentdContainer)
		podSpe.Volumes = append(podSpe.Volumes, fluentdConfigMapVolume)
	}

	return podSpe
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
			Selector: MakeElaResourceSelector(u),
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
