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
	"fmt"
	"strconv"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/queue"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeElaQueueContainer creates the container spec for queue sidecar.
func MakeElaQueueContainer(rev *v1alpha1.Revision, controllerConfig *ControllerConfig) *corev1.Container {
	configName := ""
	if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
		configName = owner.Name
	}

	const elaQueueConfigVolumeName = "queue-config"
	return &corev1.Container{
		Name:  queueContainerName,
		Image: controllerConfig.QueueSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(queueContainerCPU),
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          queue.RequestQueuePortName,
				ContainerPort: int32(queue.RequestQueuePort),
			},
			// Provides health checks and lifecycle hooks.
			{
				Name:          queue.RequestQueueAdminPortName,
				ContainerPort: int32(queue.RequestQueueAdminPort),
			},
		},
		// This handler (1) marks the service as not ready and (2)
		// adds a small delay before the container is killed.
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(queue.RequestQueueAdminPort),
					Path: queue.RequestQueueQuitPath,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(queue.RequestQueueAdminPort),
					Path: queue.RequestQueueHealthPath,
				},
			},
			// We want to mark the service as not ready as soon as the
			// PreStop handler is called, so we need to check a little
			// bit more often than the default.  It is a small
			// sacrifice for a low rate of 503s.
			PeriodSeconds: 1,
		},
		Args: []string{
			fmt.Sprintf("-concurrencyQuantumOfTime=%v", controllerConfig.AutoscaleConcurrencyQuantumOfTime.Get()),
		},
		Env: []corev1.EnvVar{
			{
				Name:  "ELA_NAMESPACE",
				Value: rev.Namespace,
			},
			{
				Name:  "ELA_CONFIGURATION",
				Value: configName,
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
			{
				Name:  "ELA_LOGGING_CONFIG",
				Value: controllerConfig.QueueProxyLoggingConfig,
			},
			{
				Name:  "ELA_LOGGING_LEVEL",
				Value: controllerConfig.QueueProxyLoggingLevel,
			},
		},
	}
}
