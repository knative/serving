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
	"math"
	"strconv"

	"k8s.io/apimachinery/pkg/api/resource"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/knative/pkg/logging"
	pkgmetrics "github.com/knative/pkg/metrics"
	"github.com/knative/pkg/ptr"
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
		Name:          v1alpha1.QueueAdminPortName,
		ContainerPort: int32(networking.QueueAdminPort),
	}, {
		Name:          v1alpha1.AutoscalingQueueMetricsPortName,
		ContainerPort: int32(networking.AutoscalingQueueMetricsPort),
	}, {
		Name:          v1alpha1.UserQueueMetricsPortName,
		ContainerPort: int32(networking.UserQueueMetricsPort),
	}}

	queueReadinessProbe = &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(networking.QueueAdminPort),
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

	queueSecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.Bool(false),
	}
)

func createQueueResources(annotations map[string]string, userContainer *corev1.Container) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{}
	resourceRequests := corev1.ResourceList{corev1.ResourceCPU: queueContainerCPU}
	resourceLimits := corev1.ResourceList{}
	ok := false
	var requestCPU, limitCPU, requestMemory, limitMemory resource.Quantity
	var resourcePercentage float32

	if ok, resourcePercentage = createResourcePercentageFromAnnotations(annotations, serving.QueueSideCarResourcePercentageAnnotation); ok {

		if ok, requestCPU = computeResourceRequirements(userContainer.Resources.Requests.Cpu(), resourcePercentage, queueContainerRequestCPU); ok {
			resourceRequests[corev1.ResourceCPU] = requestCPU
		}

		if ok, limitCPU = computeResourceRequirements(userContainer.Resources.Limits.Cpu(), resourcePercentage, queueContainerLimitCPU); ok {
			resourceLimits[corev1.ResourceCPU] = limitCPU
		}

		if ok, requestMemory = computeResourceRequirements(userContainer.Resources.Requests.Memory(), resourcePercentage, queueContainerRequestMemory); ok {
			resourceRequests[corev1.ResourceMemory] = requestMemory
		}

		if ok, limitMemory = computeResourceRequirements(userContainer.Resources.Limits.Memory(), resourcePercentage, queueContainerLimitMemory); ok {
			resourceLimits[corev1.ResourceMemory] = limitMemory
		}

	}

	resources.Requests = resourceRequests

	if len(resourceLimits) != 0 {
		resources.Limits = resourceLimits
	}

	return resources
}

func computeResourceRequirements(resourceQuantity *resource.Quantity, percentage float32, boundary resourceBoundary) (bool, resource.Quantity) {
	if resourceQuantity.IsZero() {
		return false, resource.Quantity{}
	}

	// Incase the resourceQuantity MilliValue overflow in we use MaxInt64
	// https://github.com/kubernetes/apimachinery/blob/master/pkg/api/resource/quantity.go
	scaledValue := resourceQuantity.Value()
	scaledMilliValue := int64(math.MaxInt64 - 1)
	if scaledValue < (math.MaxInt64 / 1000) {
		scaledMilliValue = resourceQuantity.MilliValue()
	}

	// float64(math.MaxInt64) > math.MaxInt64, to avoid overflow
	percentageValue := float64(scaledMilliValue) * float64(percentage)
	var newValue int64
	if percentageValue >= math.MaxInt64 {
		newValue = math.MaxInt64
	} else {
		newValue = int64(percentageValue)
	}

	newquantity := *resource.NewMilliQuantity(newValue, resource.BinarySI)
	newquantity = boundary.applyBoundary(newquantity)
	return true, newquantity
}

func createResourcePercentageFromAnnotations(m map[string]string, k string) (bool, float32) {
	v, ok := m[k]
	if !ok {
		return false, 0
	}
	value, err := strconv.ParseFloat(v, 32)
	if err != nil {
		return false, 0
	}
	return true, float32(value / 100)
}

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

	var volumeMounts []corev1.VolumeMount
	if observabilityConfig.EnableVarLogCollection {
		volumeMounts = append(volumeMounts, internalVolumeMount)
	}

	return &corev1.Container{
		Name:            QueueContainerName,
		Image:           deploymentConfig.QueueSidecarImage,
		Resources:       createQueueResources(rev.GetAnnotations(), rev.Spec.GetContainer()),
		Ports:           ports,
		ReadinessProbe:  queueReadinessProbe,
		VolumeMounts:    volumeMounts,
		SecurityContext: queueSecurityContext,
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
		}, {
			Name:  pkgmetrics.DomainEnv,
			Value: pkgmetrics.Domain(),
		}, {
			Name:  "USER_CONTAINER_NAME",
			Value: rev.Spec.GetContainer().Name,
		}, {
			Name:  "ENABLE_VAR_LOG_COLLECTION",
			Value: strconv.FormatBool(observabilityConfig.EnableVarLogCollection),
		}, {
			Name:  "VAR_LOG_VOLUME_NAME",
			Value: varLogVolumeName,
		}, {
			Name:  "INTERNAL_VOLUME_PATH",
			Value: internalVolumePath,
		}},
	}
}
