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

package resources

import (
	"fmt"
	"strconv"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	queueResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName("cpu"): queueContainerCPU,
		},
	}
	queuePorts = []corev1.ContainerPort{{
		Name:          queue.RequestQueuePortName,
		ContainerPort: int32(queue.RequestQueuePort),
	}, {
		// Provides health checks and lifecycle hooks.
		Name:          queue.RequestQueueAdminPortName,
		ContainerPort: int32(queue.RequestQueueAdminPort),
	}}
	// This handler (1) marks the service as not ready and (2)
	// adds a small delay before the container is killed.
	queueLifecycle = &corev1.Lifecycle{
		PreStop: &corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(queue.RequestQueueAdminPort),
				Path: queue.RequestQueueQuitPath,
			},
		},
	}
	queueReadinessProbe = &corev1.Probe{
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
	}
)

// makeQueueContainer creates the container spec for queue sidecar.
func makeQueueContainer(rev *v1alpha1.Revision, loggingConfig *logging.Config, autoscalerConfig *autoscaler.Config,
	controllerConfig *config.Controller) *corev1.Container {
	configName := ""
	if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
		configName = owner.Name
	}

	autoscalerAddress := "autoscaler"

	var loggingLevel string
	if ll, ok := loggingConfig.LoggingLevel["queueproxy"]; ok {
		loggingLevel = ll.String()
	}

	return &corev1.Container{
		Name:           queueContainerName,
		Image:          controllerConfig.QueueSidecarImage,
		Resources:      queueResources,
		Ports:          queuePorts,
		Lifecycle:      queueLifecycle,
		ReadinessProbe: queueReadinessProbe,
		Args: []string{
			fmt.Sprintf("-containerConcurrency=%v", rev.Spec.ContainerConcurrency),
		},
		Env: []corev1.EnvVar{{
			Name:  "SERVING_NAMESPACE",
			Value: rev.Namespace,
		}, {
			Name:  "SERVING_CONFIGURATION",
			Value: configName,
		}, {
			Name:  "SERVING_REVISION",
			Value: rev.Name,
		}, {
			Name:  "SERVING_AUTOSCALER",
			Value: autoscalerAddress,
		}, {
			Name:  "SERVING_AUTOSCALER_PORT",
			Value: strconv.Itoa(AutoscalerPort),
		}, {
			Name: "SERVING_POD",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		}, {
			Name:  "SERVING_LOGGING_CONFIG",
			Value: loggingConfig.LoggingConfig,
		}, {
			Name:  "SERVING_LOGGING_LEVEL",
			Value: loggingLevel,
		}},
	}
}
