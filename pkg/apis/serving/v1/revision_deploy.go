/*
Copyright 2020 The Knative Authors

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

package v1

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
)

const (
	VarLogVolumeName = "knative-var-log"
	VarLogVolumePath = "/var/log"
)

var (
	VarLogVolumeMount = corev1.VolumeMount{
		Name:      VarLogVolumeName,
		MountPath: VarLogVolumePath,
	}

	VarLogVolume = corev1.Volume{
		Name: VarLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	// This PreStop hook is actually calling an endpoint on the queue-proxy
	// because of the way PreStop hooks are called by kubelet. We use this
	// to block the user-container from exiting before the queue-proxy is ready
	// to exit so we can guarantee that there are no more requests in flight.
	UserLifecycle = &corev1.Lifecycle{
		PreStop: &corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(networking.QueueAdminPort),
				Path: queue.RequestQueueDrainPath,
			},
		},
	}
)

func BuildContainerPorts(userPort int32) []corev1.ContainerPort {
	return []corev1.ContainerPort{{
		Name:          UserPortName,
		ContainerPort: userPort,
	}}
}

func BuildUserPortEnv(userPort string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  "PORT",
		Value: userPort,
	}
}

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

func GetUserPort(rev *Revision) int32 {
	ports := rev.Spec.GetContainer().Ports

	if len(ports) > 0 && ports[0].ContainerPort != 0 {
		return ports[0].ContainerPort
	}

	return DefaultUserPort
}

func MakeUserContainer(rev *Revision) *corev1.Container {
	userContainer := rev.Spec.GetContainer().DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the fieldmasks / validations in pkg/apis/serving

	userContainer.VolumeMounts = append(userContainer.VolumeMounts, VarLogVolumeMount)
	userContainer.Lifecycle = UserLifecycle
	userPort := GetUserPort(rev)
	userPortInt := int(userPort)
	userPortStr := strconv.Itoa(userPortInt)
	// Replacement is safe as only up to a single port is allowed on the Revision
	userContainer.Ports = BuildContainerPorts(userPort)
	userContainer.Env = append(userContainer.Env, BuildUserPortEnv(userPortStr))
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

func MakePodSpec(rev *Revision, containers []corev1.Container) *corev1.PodSpec {
	return &corev1.PodSpec{
		Containers:                    containers,
		Volumes:                       append([]corev1.Volume{VarLogVolume}, rev.Spec.Volumes...),
		ServiceAccountName:            rev.Spec.ServiceAccountName,
		TerminationGracePeriodSeconds: rev.Spec.TimeoutSeconds,
		ImagePullSecrets:              rev.Spec.ImagePullSecrets,
	}

}
