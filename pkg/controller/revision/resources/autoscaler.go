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

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/revision/resources/names"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/system"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	loggingConfigVolume = corev1.Volume{
		Name: logging.ConfigName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: logging.ConfigName,
				},
			},
		},
	}

	autoscalerConfigVolume = corev1.Volume{
		Name: autoscaler.ConfigName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: autoscaler.ConfigName,
				},
			},
		},
	}

	autoscalerVolumes = []corev1.Volume{
		autoscalerConfigVolume,
		loggingConfigVolume,
	}

	autoscalerVolumeMounts = []corev1.VolumeMount{{
		Name:      autoscaler.ConfigName,
		MountPath: "/etc/config-autoscaler",
	}, {
		Name:      logging.ConfigName,
		MountPath: "/etc/config-logging",
	}}

	autoscalerServicePorts = []corev1.ServicePort{{
		Name:       "autoscaler-port",
		Port:       int32(AutoscalerPort),
		TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: AutoscalerPort},
	}}

	autoscalerResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("25m"),
		},
	}

	autoscalerPorts = []corev1.ContainerPort{{
		Name:          "autoscaler-port",
		ContainerPort: AutoscalerPort,
	}}
)

// MakeAutoscalerDeployment creates the deployment of the autoscaler for a particular revision.
func MakeAutoscalerDeployment(rev *v1alpha1.Revision, autoscalerImage string, replicaCount int32) *appsv1.Deployment {
	configName := ""
	if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
		configName = owner.Name
	}

	annotations := makeAnnotations(rev)
	annotations[sidecarIstioInjectAnnotation] = "true"

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.Autoscaler(rev),
			Namespace:       system.Namespace,
			Labels:          makeLabels(rev),
			Annotations:     makeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: makeSelector(rev),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      makeAutoScalerLabels(rev),
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "autoscaler",
						Image:     autoscalerImage,
						Resources: autoscalerResources,
						Ports:     autoscalerPorts,
						Env: []corev1.EnvVar{{
							Name:  "SERVING_NAMESPACE",
							Value: rev.Namespace,
						}, {
							Name:  "SERVING_DEPLOYMENT",
							Value: names.Deployment(rev),
						}, {
							Name:  "SERVING_CONFIGURATION",
							Value: configName,
						}, {
							Name:  "SERVING_REVISION",
							Value: rev.Name,
						}, {
							Name:  "SERVING_AUTOSCALER_PORT",
							Value: strconv.Itoa(AutoscalerPort),
						}},
						Args: []string{
							fmt.Sprintf("-concurrencyModel=%v", rev.Spec.ConcurrencyModel),
						},
						VolumeMounts: autoscalerVolumeMounts,
					}},
					ServiceAccountName: "autoscaler",
					Volumes:            autoscalerVolumes,
				},
			},
		},
	}
}

// MakeAutoscalerService returns a service for the autoscaler of the given revision.
func MakeAutoscalerService(rev *v1alpha1.Revision) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.Autoscaler(rev),
			Namespace:       system.Namespace,
			Labels:          makeAutoScalerLabels(rev),
			Annotations:     makeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
		},
		Spec: corev1.ServiceSpec{
			Ports: autoscalerServicePorts,
			Type:  "NodePort",
			Selector: map[string]string{
				serving.AutoscalerLabelKey: names.Autoscaler(rev),
			},
		},
	}
}

// makeAutoScalerLabels constructs the labels we will apply to
// service and deployment specs for autoscaler.
func makeAutoScalerLabels(rev *v1alpha1.Revision) map[string]string {
	labels := makeLabels(rev)
	labels[serving.AutoscalerLabelKey] = names.Autoscaler(rev)
	return labels
}
