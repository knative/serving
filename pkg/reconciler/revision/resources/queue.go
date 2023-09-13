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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgnet "knative.dev/networking/pkg/apis/networking"
	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/kmap"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	apicfg "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/queue/readiness"
	"knative.dev/serving/pkg/reconciler/revision/config"
)

const (
	localAddress              = "127.0.0.1"
	requestQueueHTTPPortName  = "queue-port"
	requestQueueHTTPSPortName = "https-port" // must be no more than 15 characters.
	profilingPortName         = "profiling-port"
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
	queueHTTPSPort = corev1.ContainerPort{
		Name:          requestQueueHTTPSPortName,
		ContainerPort: networking.BackendHTTPSPort,
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
		ReadOnlyRootFilesystem:   ptr.Bool(true),
		RunAsNonRoot:             ptr.Bool(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
)

func createQueueResources(cfg *deployment.Config, annotations map[string]string, userContainer *corev1.Container, useDefaults bool) corev1.ResourceRequirements {
	resourceRequests := corev1.ResourceList{}
	resourceLimits := corev1.ResourceList{}

	for _, r := range []struct {
		Name           corev1.ResourceName
		Request        *resource.Quantity
		RequestDefault *resource.Quantity
		Limit          *resource.Quantity
		LimitDefault   *resource.Quantity
	}{{
		Name:           corev1.ResourceCPU,
		Request:        cfg.QueueSidecarCPURequest,
		RequestDefault: &deployment.QueueSidecarCPURequestDefault,
		Limit:          cfg.QueueSidecarCPULimit,
		LimitDefault:   &deployment.QueueSidecarCPULimitDefault,
	}, {
		Name:           corev1.ResourceMemory,
		Request:        cfg.QueueSidecarMemoryRequest,
		RequestDefault: &deployment.QueueSidecarMemoryRequestDefault,
		Limit:          cfg.QueueSidecarMemoryLimit,
		LimitDefault:   &deployment.QueueSidecarMemoryLimitDefault,
	}, {
		Name:    corev1.ResourceEphemeralStorage,
		Request: cfg.QueueSidecarEphemeralStorageRequest,
		Limit:   cfg.QueueSidecarEphemeralStorageLimit,
	}} {
		if r.Request != nil {
			resourceRequests[r.Name] = *r.Request
		} else if useDefaults && r.RequestDefault != nil {
			resourceRequests[r.Name] = *r.RequestDefault
		}
		if r.Limit != nil {
			resourceLimits[r.Name] = *r.Limit
		} else if useDefaults && r.LimitDefault != nil {
			resourceLimits[r.Name] = *r.LimitDefault
		}
	}

	var requestCPU, limitCPU, requestMemory, limitMemory resource.Quantity

	if resourceFraction, ok := fractionFromPercentage(annotations, serving.QueueSidecarResourcePercentageAnnotation); ok {
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

	if requestCPU, ok := resourceFromAnnotation(annotations, serving.QueueSidecarCPUResourceRequestAnnotation); ok {
		resourceRequests[corev1.ResourceCPU] = requestCPU
	}

	if limitCPU, ok := resourceFromAnnotation(annotations, serving.QueueSidecarCPUResourceLimitAnnotation); ok {
		resourceLimits[corev1.ResourceCPU] = limitCPU
	}

	if requestMemory, ok := resourceFromAnnotation(annotations, serving.QueueSidecarMemoryResourceRequestAnnotation); ok {
		resourceRequests[corev1.ResourceMemory] = requestMemory
	}

	if limitMemory, ok := resourceFromAnnotation(annotations, serving.QueueSidecarMemoryResourceLimitAnnotation); ok {
		resourceLimits[corev1.ResourceMemory] = limitMemory
	}

	if requestEphmeralStorage, ok := resourceFromAnnotation(annotations, serving.QueueSidecarEphemeralStorageResourceRequestAnnotation); ok {
		resourceRequests[corev1.ResourceEphemeralStorage] = requestEphmeralStorage
	}

	if limitEphemeralStorage, ok := resourceFromAnnotation(annotations, serving.QueueSidecarEphemeralStorageResourceLimitAnnotation); ok {
		resourceLimits[corev1.ResourceEphemeralStorage] = limitEphemeralStorage
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

func resourceFromAnnotation(m map[string]string, key kmap.KeyPriority) (resource.Quantity, bool) {
	_, v, _ := key.Get(m)
	q, err := resource.ParseQuantity(v)
	return q, err == nil
}

func fractionFromPercentage(m map[string]string, key kmap.KeyPriority) (float64, bool) {
	_, v, _ := key.Get(m)
	value, err := strconv.ParseFloat(v, 64)
	return value / 100, err == nil
}

// makeQueueContainer creates the container spec for the queue sidecar.
func makeQueueContainer(rev *v1.Revision, cfg *config.Config) (*corev1.Container, error) {
	configName := ""
	if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
		configName = owner.Name
	}
	serviceName := rev.Labels[serving.ServiceLabelKey]

	userPort := getUserPort(rev)

	var loggingLevel string
	if ll, ok := cfg.Logging.LoggingLevel["queueproxy"]; ok {
		loggingLevel = ll.String()
	}

	ts := int64(0)
	if rev.Spec.TimeoutSeconds != nil {
		ts = *rev.Spec.TimeoutSeconds
	}
	responseStartTimeout := int64(0)
	if rev.Spec.ResponseStartTimeoutSeconds != nil {
		responseStartTimeout = *rev.Spec.ResponseStartTimeoutSeconds
	}
	idleTimeout := int64(0)
	if rev.Spec.IdleTimeoutSeconds != nil {
		idleTimeout = *rev.Spec.IdleTimeoutSeconds
	}
	ports := queueNonServingPorts
	if cfg.Observability.EnableProfiling {
		ports = append(ports, profilingPort)
	}
	// TODO(knative/serving/#4283): Eventually only one port should be needed.
	servingPort := queueHTTPPort
	if rev.GetProtocol() == pkgnet.ProtocolH2C {
		servingPort = queueHTTP2Port
	}
	ports = append(ports, servingPort, queueHTTPSPort)

	container := rev.Spec.GetContainer()

	var httpProbe, execProbe *corev1.Probe
	var userProbeJSON string
	if container.ReadinessProbe != nil {
		probePort := userPort
		if container.ReadinessProbe.HTTPGet != nil && container.ReadinessProbe.HTTPGet.Port.IntValue() != 0 {
			probePort = container.ReadinessProbe.HTTPGet.Port.IntVal
		}
		if container.ReadinessProbe.TCPSocket != nil && container.ReadinessProbe.TCPSocket.Port.IntValue() != 0 {
			probePort = container.ReadinessProbe.TCPSocket.Port.IntVal
		}

		// The activator attempts to detect readiness itself by checking the Queue
		// Proxy's health endpoint rather than waiting for Kubernetes to check and
		// propagate the Ready state. We encode the original probe as JSON in an
		// environment variable for this health endpoint to use.
		userProbe := container.ReadinessProbe.DeepCopy()
		applyReadinessProbeDefaultsForExec(userProbe, probePort)

		var err error
		userProbeJSON, err = readiness.EncodeProbe(userProbe)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize readiness probe: %w", err)
		}

		// After startup we'll directly use the same http health check endpoint the
		// execprobe would have used (which will then check the user container).
		// Unlike the StartupProbe, we don't need to override any of the other settings
		// except period here. See below.
		httpProbe = container.ReadinessProbe.DeepCopy()
		httpProbe.ProbeHandler = corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(int(servingPort.ContainerPort)),
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  netheader.ProbeKey,
					Value: queue.Name,
				}},
			},
		}
	}

	useQPResourceDefaults := cfg.Features.QueueProxyResourceDefaults == apicfg.Enabled
	c := &corev1.Container{
		Name:            QueueContainerName,
		Image:           cfg.Deployment.QueueSidecarImage,
		Resources:       createQueueResources(cfg.Deployment, rev.GetAnnotations(), container, useQPResourceDefaults),
		Ports:           ports,
		StartupProbe:    execProbe,
		ReadinessProbe:  httpProbe,
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
			Name:  "QUEUE_SERVING_TLS_PORT",
			Value: strconv.Itoa(int(queueHTTPSPort.ContainerPort)),
		}, {
			Name:  "CONTAINER_CONCURRENCY",
			Value: strconv.Itoa(int(rev.Spec.GetContainerConcurrency())),
		}, {
			Name:  "REVISION_TIMEOUT_SECONDS",
			Value: strconv.Itoa(int(ts)),
		}, {
			Name:  "REVISION_RESPONSE_START_TIMEOUT_SECONDS",
			Value: strconv.Itoa(int(responseStartTimeout)),
		}, {
			Name:  "REVISION_IDLE_TIMEOUT_SECONDS",
			Value: strconv.Itoa(int(idleTimeout)),
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
			Value: cfg.Logging.LoggingConfig,
		}, {
			Name:  "SERVING_LOGGING_LEVEL",
			Value: loggingLevel,
		}, {
			Name:  "SERVING_REQUEST_LOG_TEMPLATE",
			Value: cfg.Observability.RequestLogTemplate,
		}, {
			Name:  "SERVING_ENABLE_REQUEST_LOG",
			Value: strconv.FormatBool(cfg.Observability.EnableRequestLog),
		}, {
			Name:  "SERVING_REQUEST_METRICS_BACKEND",
			Value: cfg.Observability.RequestMetricsBackend,
		}, {
			Name:  "SERVING_REQUEST_METRICS_REPORTING_PERIOD_SECONDS",
			Value: strconv.Itoa(cfg.Observability.RequestMetricsReportingPeriodSeconds),
		}, {
			Name:  "TRACING_CONFIG_BACKEND",
			Value: string(cfg.Tracing.Backend),
		}, {
			Name:  "TRACING_CONFIG_ZIPKIN_ENDPOINT",
			Value: cfg.Tracing.ZipkinEndpoint,
		}, {
			Name:  "TRACING_CONFIG_DEBUG",
			Value: strconv.FormatBool(cfg.Tracing.Debug),
		}, {
			Name:  "TRACING_CONFIG_SAMPLE_RATE",
			Value: fmt.Sprint(cfg.Tracing.SampleRate),
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
			Value: userProbeJSON,
		}, {
			Name:  "ENABLE_PROFILING",
			Value: strconv.FormatBool(cfg.Observability.EnableProfiling),
		}, {
			Name:  "SERVING_ENABLE_PROBE_REQUEST_LOG",
			Value: strconv.FormatBool(cfg.Observability.EnableProbeRequestLog),
		}, {
			Name:  "METRICS_COLLECTOR_ADDRESS",
			Value: cfg.Observability.MetricsCollectorAddress,
		}, {
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "status.hostIP",
				},
			},
		}, {
			Name:  "ENABLE_HTTP2_AUTO_DETECTION",
			Value: strconv.FormatBool(cfg.Features.AutoDetectHTTP2 == apicfg.Enabled),
		}, {
			Name:  "ROOT_CA",
			Value: cfg.Deployment.QueueSidecarRootCA,
		}},
	}

	return c, nil
}

func applyReadinessProbeDefaultsForExec(p *corev1.Probe, port int32) {
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
			Name:  netheader.KubeletProbeKey,
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
