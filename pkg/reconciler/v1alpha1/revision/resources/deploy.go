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

	"github.com/knative/pkg/kmeta"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const varLogVolumeName = "varlog"

var (
	varLogVolume = corev1.Volume{
		Name: varLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	varLogVolumeMount = corev1.VolumeMount{
		Name:      varLogVolumeName,
		MountPath: "/var/log",
	}

	userPorts = []corev1.ContainerPort{{
		Name:          userPortName,
		ContainerPort: int32(userPort),
	}}

	// Expose containerPort as env PORT.
	userEnv = corev1.EnvVar{
		Name:  userPortEnvName,
		Value: strconv.Itoa(userPort),
	}

	userResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: userContainerCPU,
		},
	}

	// Add our own PreStop hook here, which should do two things:
	// - make the container fails the next readinessCheck to avoid
	//   having more traffic, and
	// - add a small delay so that the container stays alive a little
	//   bit longer in case stoppage of traffic is not effective
	//   immediately.
	userLifecycle = &corev1.Lifecycle{
		PreStop: &corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(queue.RequestQueueAdminPort),
				Path: queue.RequestQueueQuitPath,
			},
		},
	}
)

func rewriteUserProbe(p *corev1.Probe) {
	if p == nil {
		return
	}
	switch {
	case p.HTTPGet != nil:
		// For HTTP probes, we route them through the queue container
		// so that we know the queue proxy is ready/live as well.
		p.HTTPGet.Port = intstr.FromInt(queue.RequestQueuePort)
	case p.TCPSocket != nil:
		p.TCPSocket.Port = intstr.FromInt(userPort)
	}
}

func makePodSpec(rev *v1alpha1.Revision, loggingConfig *logging.Config, observabilityConfig *config.Observability, autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *corev1.PodSpec {
	userContainer := rev.Spec.Container.DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the validations in pkg/webhook.validateContainer.
	userContainer.Name = userContainerName
	userContainer.Resources = userResources
	userContainer.Ports = userPorts
	userContainer.VolumeMounts = append(userContainer.VolumeMounts, varLogVolumeMount)
	userContainer.Lifecycle = userLifecycle
	userContainer.Env = append(userContainer.Env, userEnv)
	userContainer.Env = append(userContainer.Env, getKnativeEnvVar(rev)...)
	// Prefer imageDigest from revision if available
	if rev.Status.ImageDigest != "" {
		userContainer.Image = rev.Status.ImageDigest
	}

	// If the client provides probes, we should fill in the port for them.
	rewriteUserProbe(userContainer.ReadinessProbe)
	rewriteUserProbe(userContainer.LivenessProbe)

	podSpec := &corev1.PodSpec{
		Containers: []corev1.Container{
			*userContainer,
			*makeQueueContainer(rev, loggingConfig, autoscalerConfig, controllerConfig),
		},
		Volumes:            []corev1.Volume{varLogVolume},
		ServiceAccountName: rev.Spec.ServiceAccountName,
	}

	// Add Fluentd sidecar and its config map volume if var log collection is enabled.
	if observabilityConfig.EnableVarLogCollection {
		podSpec.Containers = append(podSpec.Containers, *makeFluentdContainer(rev, observabilityConfig))
		podSpec.Volumes = append(podSpec.Volumes, *makeFluentdConfigMapVolume(rev))
	}

	return podSpec
}

func MakeDeployment(rev *v1alpha1.Revision,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *appsv1.Deployment {

	podTemplateAnnotations := makeAnnotations(rev)
	// TODO(nghia): Remove the need for this
	podTemplateAnnotations[sidecarIstioInjectAnnotation] = "true"
	// TODO(mattmoor): Once we have a mechanism for decorating arbitrary deployments (and opting
	// out via annotation) we should explicitly disable that here to avoid redundant Image
	// resources.

	// Inject the IP ranges for istio sidecar configuration.
	// We will inject this value only if all of the following are true:
	// - the config map contains a non-empty value
	// - the user doesn't specify this annotation in configuration's pod template
	// - configured values are valid CIDR notation IP addresses
	// If these conditions are not met, this value will be left untouched.
	// * is a special value that is accepted as a valid.
	// * intercepts calls to all IPs: in cluster as well as outside the cluster.
	if _, ok := podTemplateAnnotations[IstioOutboundIPRangeAnnotation]; !ok {
		if len(networkConfig.IstioOutboundIPRanges) > 0 {
			podTemplateAnnotations[IstioOutboundIPRangeAnnotation] = networkConfig.IstioOutboundIPRanges
		}
	}

	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.Deployment(rev),
			Namespace:       rev.Namespace,
			Labels:          makeLabels(rev),
			Annotations:     makeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                &one,
			Selector:                makeSelector(rev),
			ProgressDeadlineSeconds: &ProgressDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      makeLabels(rev),
					Annotations: podTemplateAnnotations,
				},
				Spec: *makePodSpec(rev, loggingConfig, observabilityConfig, autoscalerConfig, controllerConfig),
			},
		},
	}
}
