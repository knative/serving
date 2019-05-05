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
	"strconv"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/deployment"
	"github.com/knative/serving/pkg/metrics"
	"github.com/knative/serving/pkg/queue"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const requestQueueHTTPPortName = "queue-port"

var (
	queueResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName("cpu"): queueContainerCPU,
		},
	}

	queueHTTPPort = corev1.ContainerPort{
		Name:          requestQueueHTTPPortName,
		ContainerPort: int32(networking.BackendHTTPPort),
	}
	queueHTTP2Port = corev1.ContainerPort{
		Name:          requestQueueHTTPPortName,
		ContainerPort: int32(networking.BackendHTTP2Port),
	}
	queueNonServingPorts = []corev1.ContainerPort{{
		// Provides health checks and lifecycle hooks.
		Name:          v1alpha1.RequestQueueAdminPortName,
		ContainerPort: int32(networking.RequestQueueAdminPort),
	}, {
		Name:          v1alpha1.RequestQueueMetricsPortName,
		ContainerPort: int32(networking.RequestQueueMetricsPort),
	}}

	queueReadinessProbe = &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(networking.RequestQueueAdminPort),
				Path: queue.RequestQueueHealthPath,
			},
		},
		// We want to mark the service as not ready as soon as the
		// PreStop handler is called, so we need to check a little
		// bit more often than the default.  It is a small
		// sacrifice for a low rate of 503s.
		PeriodSeconds: 1,
		// We keep the connection open for a while because we're
		// actively probing the user-container on that endpoint and
		// thus don't want to be limited by K8s granularity here.
		TimeoutSeconds: 10,
	}
)

// makeQueueContainer creates the container spec for the queue sidecar.
func makeQueueContainer(rev *v1alpha1.Revision, loggingConfig *logging.Config, observabilityConfig *metrics.ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, deploymentConfig *deployment.Config) *corev1.Container {
	configName := ""
	if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
		configName = owner.Name
	}
	serviceName := rev.Labels[serving.ServiceLabelKey]

	userPort := getUserPort(rev)

	var loggingLevel string
	if ll, ok := loggingConfig.LoggingLevel["queueproxy"]; ok {
		loggingLevel = ll.String()
	}

	ts := int64(0)
	if rev.Spec.TimeoutSeconds != nil {
		ts = *rev.Spec.TimeoutSeconds
	}

	// We need to configure only one serving port for the Queue proxy, since
	// we know the protocol that is being used by this application.
	ports := queueNonServingPorts
	if rev.GetProtocol() == networking.ProtocolH2C {
		ports = append(ports, queueHTTP2Port)
	} else {
		ports = append(ports, queueHTTPPort)
	}

	return &corev1.Container{
		Name:           QueueContainerName,
		Image:          deploymentConfig.QueueSidecarImage,
		Resources:      queueResources,
		Ports:          ports,
		ReadinessProbe: queueReadinessProbe,
		Env: []corev1.EnvVar{{
			Name:  "SERVING_NAMESPACE",
			Value: rev.Namespace,
		}, {
			Name:  "SERVING_SERVICE",
			Value: serviceName,
		}, {
			Name:  "SERVING_CONFIGURATION",
			Value: configName,
		}, {
			Name:  "SERVING_REVISION",
			Value: rev.Name,
		}, {
			Name:  "QUEUE_SERVING_PORT",
			Value: strconv.Itoa(int(ports[len(ports)-1].ContainerPort)),
		}, {
			Name:  "CONTAINER_CONCURRENCY",
			Value: strconv.Itoa(int(rev.Spec.ContainerConcurrency)),
		}, {
			Name:  "REVISION_TIMEOUT_SECONDS",
			Value: strconv.Itoa(int(ts)),
		}, {
			Name: "SERVING_POD",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		}, {
			Name: "SERVING_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		}, {
			Name:  "SERVING_LOGGING_CONFIG",
			Value: loggingConfig.LoggingConfig,
		}, {
			Name:  "SERVING_LOGGING_LEVEL",
			Value: loggingLevel,
		}, {
			Name:  "SERVING_REQUEST_LOG_TEMPLATE",
			Value: observabilityConfig.RequestLogTemplate,
		}, {
			Name:  "SERVING_REQUEST_METRICS_BACKEND",
			Value: observabilityConfig.RequestMetricsBackend,
		}, {
			Name:  "USER_PORT",
			Value: strconv.Itoa(int(userPort)),
		}, {
			Name:  system.NamespaceEnvKey,
			Value: system.Namespace(),
		}},
	}
}
