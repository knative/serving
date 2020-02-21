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

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/pkg/resources"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	internalVolumeName = "knative-internal"
	internalVolumePath = "/var/knative-internal"
	podInfoVolumeName  = "podinfo"
	podInfoVolumePath  = "/etc/podinfo"
	metadataLabelsRef  = "metadata.labels"
	metadataLabelsPath = "labels"
)

var (
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
)

func makePodSpec(rev *v1.Revision, loggingConfig *logging.Config, tracingConfig *tracingconfig.Config, observabilityConfig *metrics.ObservabilityConfig, autoscalerConfig *autoscalerconfig.Config, deploymentConfig *deployment.Config) (*corev1.PodSpec, error) {
	userContainer := v1.MakeUserContainer(rev)
	queueContainer, err := makeQueueContainer(rev, loggingConfig, tracingConfig, observabilityConfig, autoscalerConfig, deploymentConfig)

	if err != nil {
		return nil, fmt.Errorf("failed to create queue-proxy container: %w", err)
	}

	podSpec := v1.MakePodSpec(rev, []corev1.Container{*userContainer, *queueContainer})

	// Add the Knative internal volume only if /var/log collection is enabled
	if observabilityConfig.EnableVarLogCollection {
		podSpec.Volumes = append(podSpec.Volumes, internalVolume)
	}

	if autoscalerConfig.EnableGracefulScaledown {
		podSpec.Volumes = append(podSpec.Volumes, labelVolume)
	}

	return podSpec, nil
}

// MakeDeployment constructs a K8s Deployment resource from a revision.
func MakeDeployment(rev *v1.Revision,
	loggingConfig *logging.Config, tracingConfig *tracingconfig.Config, networkConfig *network.Config, observabilityConfig *metrics.ObservabilityConfig,
	autoscalerConfig *autoscalerconfig.Config, deploymentConfig *deployment.Config) (*appsv1.Deployment, error) {

	podTemplateAnnotations := resources.FilterMap(rev.GetAnnotations(), func(k string) bool {
		return k == serving.RevisionLastPinnedAnnotationKey
	})

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
	podSpec, err := makePodSpec(rev, loggingConfig, tracingConfig, observabilityConfig, autoscalerConfig, deploymentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create PodSpec: %w", err)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Deployment(rev),
			Namespace: rev.Namespace,
			Labels:    makeLabels(rev),
			Annotations: resources.FilterMap(rev.GetAnnotations(), func(k string) bool {
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
