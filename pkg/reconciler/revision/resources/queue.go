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
	"math"
	"strconv"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/readiness"
	tracingconfig "knative.dev/serving/pkg/tracing/config"
)

const (
	localAddress             = "127.0.0.1"
	requestQueueHTTPPortName = "queue-port"
)

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

	queueSecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.Bool(false),
	}
)

func createQueueResources(annotations map[string]string, userContainer *corev1.Container) corev1.ResourceRequirements {
	resourceRequests := corev1.ResourceList{corev1.ResourceCPU: queueContainerCPU}
	resourceLimits := corev1.ResourceList{}
	var requestCPU, limitCPU, requestMemory, limitMemory resource.Quantity

	if resourceFraction, ok := fractionFromPercentage(annotations, serving.QueueSideCarResourcePercentageAnnotation); ok {
		if ok, requestCPU = computeResourceRequirements(userContainer.Resources.Requests.Cpu(), resourceFraction, queueContainerRequestCPU); ok {
			resourceRequests[corev1.ResourceCPU] = requestCPU
		}

		if ok, limitCPU = computeResourceRequirements(userContainer.Resources.Limits.Cpu(), resourceFraction, queueContainerLimitCPU); ok {
			resourceLimits[corev1.ResourceCPU] = limitCPU
		}

		if ok, requestMemory = computeResourceRequirements(userContainer.Resources.Requests.Memory(), resourceFraction, queueContainerRequestMemory); ok {
			resourceRequests[corev1.ResourceMemory] = requestMemory
		}

		if ok, limitMemory = computeResourceRequirements(userContainer.Resources.Limits.Memory(), resourceFraction, queueContainerLimitMemory); ok {
			resourceLimits[corev1.ResourceMemory] = limitMemory
		}
	}

	resources := corev1.ResourceRequirements{
		Requests: resourceRequests,
	}
	if len(resourceLimits) != 0 {
		resources.Limits = resourceLimits
	}

	return resources
}

func computeResourceRequirements(resourceQuantity *resource.Quantity, fraction float64, boundary resourceBoundary) (bool, resource.Quantity) {
	if resourceQuantity.IsZero() {
		return false, resource.Quantity{}
	}

	// In case the resourceQuantity MilliValue overflows int64 we use MaxInt64
	// https://github.com/kubernetes/apimachinery/blob/master/pkg/api/resource/quantity.go
	scaledValue := resourceQuantity.Value()
	scaledMilliValue := int64(math.MaxInt64 - 1)
	if scaledValue < (math.MaxInt64 / 1000) {
		scaledMilliValue = resourceQuantity.MilliValue()
	}

	// float64(math.MaxInt64) > math.MaxInt64, to avoid overflow
	percentageValue := float64(scaledMilliValue) * fraction
	newValue := int64(math.MaxInt64)
	if percentageValue < math.MaxInt64 {
		newValue = int64(percentageValue)
	}

	newquantity := boundary.applyBoundary(*resource.NewMilliQuantity(newValue, resource.BinarySI))
	return true, newquantity
}

func fractionFromPercentage(m map[string]string, k string) (float64, bool) {
	value, err := strconv.ParseFloat(m[k], 64)
	return float64(value / 100), err == nil
}

func makeQueueProbe(in *corev1.Probe) *corev1.Probe {
	if in == nil || in.PeriodSeconds == 0 {
		out := &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe-period", "0"},
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

		if in != nil {
			out.InitialDelaySeconds = in.InitialDelaySeconds
		}
		return out
	}

	timeout := 1

	if in.TimeoutSeconds > 1 {
		timeout = int(in.TimeoutSeconds)
	}

	return &corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"/ko-app/queue", "-probe-period", strconv.Itoa(timeout)},
			},
		},
		PeriodSeconds:       in.PeriodSeconds,
		TimeoutSeconds:      int32(timeout),
		SuccessThreshold:    in.SuccessThreshold,
		FailureThreshold:    in.FailureThreshold,
		InitialDelaySeconds: in.InitialDelaySeconds,
	}
}

// makeQueueContainer creates the container spec for the queue sidecar.
func makeQueueContainer(rev *v1alpha1.Revision, loggingConfig *logging.Config, tracingConfig *tracingconfig.Config, observabilityConfig *metrics.ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, deploymentConfig *deployment.Config) (*corev1.Container, error) {
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

	rp := rev.Spec.GetContainer().ReadinessProbe.DeepCopy()

	applyReadinessProbeDefaults(rp, userPort)

	probeJSON, err := readiness.EncodeProbe(rp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to serialize readiness probe")
	}

	return &corev1.Container{
		Name:            QueueContainerName,
		Image:           deploymentConfig.QueueSidecarImage,
		Resources:       createQueueResources(rev.GetAnnotations(), rev.Spec.GetContainer()),
		Ports:           ports,
		ReadinessProbe:  makeQueueProbe(rp),
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
			Value: strconv.Itoa(int(rev.Spec.GetContainerConcurrency())),
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
			Name:  "TRACING_CONFIG_ENABLE",
			Value: strconv.FormatBool(tracingConfig.Enable),
		}, {
			Name:  "TRACING_CONFIG_ZIPKIN_ENDPOINT",
			Value: tracingConfig.ZipkinEndpoint,
		}, {
			Name:  "TRACING_CONFIG_DEBUG",
			Value: strconv.FormatBool(tracingConfig.Debug),
		}, {
			Name:  "TRACING_CONFIG_SAMPLE_RATE",
			Value: fmt.Sprintf("%f", tracingConfig.SampleRate),
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
		}, {
			Name:  "SERVING_READINESS_PROBE",
			Value: probeJSON,
		}},
	}, nil
}
func applyReadinessProbeDefaults(p *corev1.Probe, port int32) {
	switch {
	case p == nil:
		return
	case p.HTTPGet != nil:
		p.HTTPGet.Host = localAddress
		p.HTTPGet.Port = intstr.FromInt(int(port))

		if p.HTTPGet.Scheme == "" {
			p.HTTPGet.Scheme = corev1.URISchemeHTTP
		}

		p.HTTPGet.HTTPHeaders = append(p.HTTPGet.HTTPHeaders, corev1.HTTPHeader{
			Name:  network.KubeletProbeHeaderName,
			Value: queue.Name,
		})
	case p.TCPSocket != nil:
		p.TCPSocket.Host = localAddress
		p.TCPSocket.Port = intstr.FromInt(int(port))
	case p.Exec != nil:
		//User-defined ExecProbe will still be run on user-container.
		p.Exec = nil
	}

	if p.PeriodSeconds > 0 && p.TimeoutSeconds < 1 {
		p.TimeoutSeconds = 1
	}
}
