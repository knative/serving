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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/readiness"
)

const (
	localAddress             = "127.0.0.1"
	requestQueueHTTPPortName = "queue-port"
	profilingPortName        = "profiling-port"
)

var (
	queueHTTPPort = corev1.ContainerPort{
		Name:          requestQueueHTTPPortName,
		ContainerPort: networking.BackendHTTPPort,
	}
	queueHTTP2Port = corev1.ContainerPort{
		Name:          requestQueueHTTPPortName,
		ContainerPort: networking.BackendHTTP2Port,
	}
	queueNonServingPorts = []corev1.ContainerPort{{
		// Provides health checks and lifecycle hooks.
		Name:          v1.QueueAdminPortName,
		ContainerPort: networking.QueueAdminPort,
	}, {
		Name:          v1.AutoscalingQueueMetricsPortName,
		ContainerPort: networking.AutoscalingQueueMetricsPort,
	}, {
		Name:          v1.UserQueueMetricsPortName,
		ContainerPort: networking.UserQueueMetricsPort,
	}}

	profilingPort = corev1.ContainerPort{
		Name:          profilingPortName,
		ContainerPort: profiling.ProfilingPort,
	}

	queueSecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.Bool(false),
	}
)

func createQueueResources(cfg *deployment.Config, annotations map[string]string, userContainer *corev1.Container) corev1.ResourceRequirements {
	resourceRequests := corev1.ResourceList{}
	resourceLimits := corev1.ResourceList{}

	for _, r := range []struct {
		Name    corev1.ResourceName
		Request *resource.Quantity
		Limit   *resource.Quantity
	}{{
		Name:    corev1.ResourceCPU,
		Request: cfg.QueueSidecarCPURequest,
		Limit:   cfg.QueueSidecarCPULimit,
	}, {
		Name:    corev1.ResourceMemory,
		Request: cfg.QueueSidecarMemoryRequest,
		Limit:   cfg.QueueSidecarMemoryLimit,
	}, {
		Name:    corev1.ResourceEphemeralStorage,
		Request: cfg.QueueSidecarEphemeralStorageRequest,
		Limit:   cfg.QueueSidecarEphemeralStorageLimit,
	}} {
		if r.Request != nil {
			resourceRequests[r.Name] = *r.Request
		}
		if r.Limit != nil {
			resourceLimits[r.Name] = *r.Limit
		}
	}

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
	return value / 100, err == nil
}

func makeQueueProbe(in *corev1.Probe) *corev1.Probe {
	if in == nil || in.PeriodSeconds == 0 {
		out := &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe-period", "0"},
				},
			},
			// The exec probe enables us to retry failed probes quickly to get sub-second
			// resolution and achieve faster cold-starts.  However, for draining pods as
			// part of the K8s lifecycle this period will bound the tail of how quickly
			// we can remove a Pod's endpoint from the K8s service.
			//
			// The trade-off here is that exec probes cost CPU to run, and for idle pods
			// (e.g. due to minScale) we see ~50m/{period} of idle CPU usage in the
			// queue-proxy.  So while setting this to 1s results in slightly faster drains
			// it also means that in the steady state the queue-proxy is consuming 10x
			// more CPU due to probes than with a period of 10s.
			//
			// See also: https://github.com/knative/serving/issues/8147
			PeriodSeconds: 10,
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

	timeout := time.Second
	if in.TimeoutSeconds > 1 {
		timeout = time.Duration(in.TimeoutSeconds) * time.Second
	}

	return &corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"/ko-app/queue", "-probe-period", timeout.String()},
			},
		},
		PeriodSeconds:       in.PeriodSeconds,
		TimeoutSeconds:      int32(timeout.Seconds()),
		SuccessThreshold:    in.SuccessThreshold,
		FailureThreshold:    in.FailureThreshold,
		InitialDelaySeconds: in.InitialDelaySeconds,
	}
}

// makeQueueContainer creates the container spec for the queue sidecar.
func makeQueueContainer(rev *v1.Revision, loggingConfig *logging.Config, tracingConfig *tracingconfig.Config, observabilityConfig *metrics.ObservabilityConfig, deploymentConfig *deployment.Config) (*corev1.Container, error) {
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

	ports := queueNonServingPorts
	if observabilityConfig.EnableProfiling {
		ports = append(ports, profilingPort)
	}
	// We need to configure only one serving port for the Queue proxy, since
	// we know the protocol that is being used by this application.
	servingPort := queueHTTPPort
	if rev.GetProtocol() == networking.ProtocolH2C {
		servingPort = queueHTTP2Port
	}
	ports = append(ports, servingPort)

	container := rev.Spec.GetContainer()
	rp := container.ReadinessProbe.DeepCopy()

	applyReadinessProbeDefaults(rp, userPort)

	probeJSON, err := readiness.EncodeProbe(rp)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize readiness probe: %w", err)
	}

	return &corev1.Container{
		Name:            QueueContainerName,
		Image:           deploymentConfig.QueueSidecarImage,
		Resources:       createQueueResources(deploymentConfig, rev.GetAnnotations(), container),
		Ports:           ports,
		ReadinessProbe:  makeQueueProbe(rp),
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
			Value: strconv.Itoa(int(servingPort.ContainerPort)),
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
			Name:  "SERVING_ENABLE_REQUEST_LOG",
			Value: strconv.FormatBool(observabilityConfig.EnableRequestLog),
		}, {
			Name:  "SERVING_REQUEST_METRICS_BACKEND",
			Value: observabilityConfig.RequestMetricsBackend,
		}, {
			Name:  "TRACING_CONFIG_BACKEND",
			Value: string(tracingConfig.Backend),
		}, {
			Name:  "TRACING_CONFIG_ZIPKIN_ENDPOINT",
			Value: tracingConfig.ZipkinEndpoint,
		}, {
			Name:  "TRACING_CONFIG_STACKDRIVER_PROJECT_ID",
			Value: tracingConfig.StackdriverProjectID,
		}, {
			Name:  "TRACING_CONFIG_DEBUG",
			Value: strconv.FormatBool(tracingConfig.Debug),
		}, {
			Name:  "TRACING_CONFIG_SAMPLE_RATE",
			Value: fmt.Sprint(tracingConfig.SampleRate),
		}, {
			Name:  "USER_PORT",
			Value: strconv.Itoa(int(userPort)),
		}, {
			Name:  system.NamespaceEnvKey,
			Value: system.Namespace(),
		}, {
			Name:  metrics.DomainEnv,
			Value: metrics.Domain(),
		}, {
			Name:  "SERVING_READINESS_PROBE",
			Value: probeJSON,
		}, {
			Name:  "ENABLE_PROFILING",
			Value: strconv.FormatBool(observabilityConfig.EnableProfiling),
		}, {
			Name:  "SERVING_ENABLE_PROBE_REQUEST_LOG",
			Value: strconv.FormatBool(observabilityConfig.EnableProbeRequestLog),
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
		// User-defined ExecProbe will still be run on user-container.
		// Use TCP probe in queue-proxy.
		p.TCPSocket = &corev1.TCPSocketAction{
			Host: localAddress,
			Port: intstr.FromInt(int(port)),
		}
		p.Exec = nil
	}

	if p.PeriodSeconds > 0 && p.TimeoutSeconds < 1 {
		p.TimeoutSeconds = 1
	}
}
