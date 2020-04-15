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

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	varLogVolumeName   = "knative-var-log"
	varLogVolumePath   = "/var/log"
	internalVolumeName = "knative-internal"
	internalVolumePath = "/var/knative-internal"
	podInfoVolumeName  = "podinfo"
	podInfoVolumePath  = "/etc/podinfo"
	metadataLabelsRef  = "metadata.labels"
	metadataLabelsPath = "labels"
)

var (
	varLogVolume = corev1.Volume{
		Name: varLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	varLogVolumeMount = corev1.VolumeMount{
		Name:      varLogVolumeName,
		MountPath: varLogVolumePath,
	}

	labelVolume = corev1.Volume{
		Name: podInfoVolumeName,
		VolumeSource: corev1.VolumeSource{
			DownwardAPI: &corev1.DownwardAPIVolumeSource{
				Items: []corev1.DownwardAPIVolumeFile{
					{
						Path: metadataLabelsPath,
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: fmt.Sprintf("%s['%s']", metadataLabelsRef, autoscaling.PreferForScaleDownLabelKey),
						},
					},
				},
			},
		},
	}

	labelVolumeMount = corev1.VolumeMount{
		Name:      podInfoVolumeName,
		MountPath: podInfoVolumePath,
	}

	internalVolume = corev1.Volume{
		Name: internalVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	internalVolumeMount = corev1.VolumeMount{
		Name:      internalVolumeName,
		MountPath: internalVolumePath,
	}

	// This PreStop hook is actually calling an endpoint on the queue-proxy
	// because of the way PreStop hooks are called by kubelet. We use this
	// to block the user-container from exiting before the queue-proxy is ready
	// to exit so we can guarantee that there are no more requests in flight.
	userLifecycle = &corev1.Lifecycle{
		PreStop: &corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(networking.QueueAdminPort),
				Path: queue.RequestQueueDrainPath,
			},
		},
	}
)

func rewriteUserProbe(p *corev1.Probe, userPort int) {
	if p == nil {
		return
	}
	switch {
	case p.HTTPGet != nil:
		// For HTTP probes, we route them through the queue container
		// so that we know the queue proxy is ready/live as well.
		// It doesn't matter to which queue serving port we are forwarding the probe.
		p.HTTPGet.Port = intstr.FromInt(networking.BackendHTTPPort)
		// With mTLS enabled, Istio rewrites probes, but doesn't spoof the kubelet
		// user agent, so we need to inject an extra header to be able to distinguish
		// between probes and real requests.
		p.HTTPGet.HTTPHeaders = append(p.HTTPGet.HTTPHeaders, corev1.HTTPHeader{
			Name:  network.KubeletProbeHeaderName,
			Value: "queue",
		})
	case p.TCPSocket != nil:
		p.TCPSocket.Port = intstr.FromInt(userPort)
	}
}

func makePodSpec(rev *v1.Revision, loggingConfig *logging.Config, tracingConfig *tracingconfig.Config, observabilityConfig *metrics.ObservabilityConfig, autoscalerConfig *autoscalerconfig.Config, deploymentConfig *deployment.Config) (*corev1.PodSpec, error) {
	queueContainer, err := makeQueueContainer(rev, loggingConfig, tracingConfig, observabilityConfig, autoscalerConfig, deploymentConfig)

	if err != nil {
		return nil, fmt.Errorf("failed to create queue-proxy container: %w", err)
	}

	userContainer := BuildUserContainer(rev)
	podSpec := BuildPodSpec(rev, []corev1.Container{*userContainer, *queueContainer})

	// Add the Knative internal volume only if /var/log collection is enabled
	if observabilityConfig.EnableVarLogCollection {
		podSpec.Volumes = append(podSpec.Volumes, internalVolume)
	}

	if autoscalerConfig.EnableGracefulScaledown {
		podSpec.Volumes = append(podSpec.Volumes, labelVolume)
	}

	return podSpec, nil
}

// BuildUserContainer makes a container from the Revision template.
func BuildUserContainer(rev *v1.Revision) *corev1.Container {
	userContainer := rev.Spec.GetContainer().DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the fieldmasks / validations in pkg/apis/serving

	userContainer.VolumeMounts = append(userContainer.VolumeMounts, varLogVolumeMount)
	userContainer.Lifecycle = userLifecycle
	userPort := getUserPort(rev)
	userPortInt := int(userPort)
	userPortStr := strconv.Itoa(userPortInt)
	// Replacement is safe as only up to a single port is allowed on the Revision
	userContainer.Ports = buildContainerPorts(userPort)
	userContainer.Env = append(userContainer.Env, buildUserPortEnv(userPortStr))
	userContainer.Env = append(userContainer.Env, getKnativeEnvVar(rev)...)
	// Explicitly disable stdin and tty allocation
	userContainer.Stdin = false
	userContainer.TTY = false

	// Prefer imageDigest from revision if available
	if rev.Status.ImageDigest != "" {
		userContainer.Image = rev.Status.ImageDigest
	}

	if userContainer.TerminationMessagePolicy == "" {
		userContainer.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
	}

	if userContainer.ReadinessProbe != nil {
		if userContainer.ReadinessProbe.HTTPGet != nil || userContainer.ReadinessProbe.TCPSocket != nil {
			// HTTP and TCP ReadinessProbes are executed by the queue-proxy directly against the
			// user-container instead of via kubelet.
			userContainer.ReadinessProbe = nil
		}
	}

	// If the client provides probes, we should fill in the port for them.
	rewriteUserProbe(userContainer.LivenessProbe, userPortInt)
	return userContainer
}

// BuildPodSpec creates a PodSpec from the given revision and containers.
func BuildPodSpec(rev *v1.Revision, containers []corev1.Container) *corev1.PodSpec {
	return &corev1.PodSpec{
		Containers:                    containers,
		Volumes:                       append([]corev1.Volume{varLogVolume}, rev.Spec.Volumes...),
		ServiceAccountName:            rev.Spec.ServiceAccountName,
		TerminationGracePeriodSeconds: rev.Spec.TimeoutSeconds,
		ImagePullSecrets:              rev.Spec.ImagePullSecrets,
	}
}

func getUserPort(rev *v1.Revision) int32 {
	ports := rev.Spec.GetContainer().Ports

	if len(ports) > 0 && ports[0].ContainerPort != 0 {
		return ports[0].ContainerPort
	}

	return v1.DefaultUserPort
}

func buildContainerPorts(userPort int32) []corev1.ContainerPort {
	return []corev1.ContainerPort{{
		Name:          v1.UserPortName,
		ContainerPort: userPort,
	}}
}

func buildUserPortEnv(userPort string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  "PORT",
		Value: userPort,
	}
}

// MakeDeployment constructs a K8s Deployment resource from a revision.
func MakeDeployment(rev *v1.Revision,
	loggingConfig *logging.Config, tracingConfig *tracingconfig.Config, networkConfig *network.Config, observabilityConfig *metrics.ObservabilityConfig,
	autoscalerConfig *autoscalerconfig.Config, deploymentConfig *deployment.Config) (*appsv1.Deployment, error) {

	podTemplateAnnotations := kmeta.FilterMap(rev.GetAnnotations(), func(k string) bool {
		return k == serving.RevisionLastPinnedAnnotationKey
	})

	podSpec, err := makePodSpec(rev, loggingConfig, tracingConfig, observabilityConfig, autoscalerConfig, deploymentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create PodSpec: %w", err)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Deployment(rev),
			Namespace: rev.Namespace,
			Labels:    makeLabels(rev),
			Annotations: kmeta.FilterMap(rev.GetAnnotations(), func(k string) bool {
				// Exclude the heartbeat label, which can have high variance.
				return k == serving.RevisionLastPinnedAnnotationKey
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                ptr.Int32(1),
			Selector:                makeSelector(rev),
			ProgressDeadlineSeconds: ptr.Int32(ProgressDeadlineSeconds),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      makeLabels(rev),
					Annotations: podTemplateAnnotations,
				},
				Spec: *podSpec,
			},
		},
	}, nil
}
