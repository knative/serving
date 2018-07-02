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

package revision

import (
	"fmt"
	"strconv"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/revision/resources"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeServingAutoscalerDeployment creates the deployment of the
// autoscaler for a particular revision.
func MakeServingAutoscalerDeployment(rev *v1alpha1.Revision, autoscalerImage string, replicaCount int32) *appsv1.Deployment {
	configName := ""
	if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
		configName = owner.Name
	}

	annotations := resources.MakeAnnotations(rev)
	annotations[sidecarIstioInjectAnnotation] = "true"

	const autoscalerConfigName = "config-autoscaler"
	autoscalerConfigVolume := corev1.Volume{
		Name: autoscalerConfigName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: autoscalerConfigName,
				},
			},
		},
	}

	const loggingConfigName = "config-logging"
	loggingConfigVolume := corev1.Volume{
		Name: loggingConfigName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: loggingConfigName,
				},
			},
		},
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            controller.GetRevisionAutoscalerName(rev),
			Namespace:       pkg.GetServingSystemNamespace(),
			Labels:          resources.MakeLabels(rev),
			Annotations:     resources.MakeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: resources.MakeSelector(rev),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      makeServingAutoScalerLabels(rev),
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "autoscaler",
						Image: autoscalerImage,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceName("cpu"): resource.MustParse("25m"),
							},
						},
						Ports: []corev1.ContainerPort{{
							Name:          "autoscaler-port",
							ContainerPort: resources.AutoscalerPort,
						}},
						Env: []corev1.EnvVar{{
							Name:  "SERVING_NAMESPACE",
							Value: rev.Namespace,
						}, {
							Name:  "SERVING_DEPLOYMENT",
							Value: controller.GetRevisionDeploymentName(rev),
						}, {
							Name:  "SERVING_CONFIGURATION",
							Value: configName,
						}, {
							Name:  "SERVING_REVISION",
							Value: rev.Name,
						}, {
							Name:  "SERVING_AUTOSCALER_PORT",
							Value: strconv.Itoa(resources.AutoscalerPort),
						}},
						Args: []string{
							fmt.Sprintf("-concurrencyModel=%v", rev.Spec.ConcurrencyModel),
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      autoscalerConfigName,
							MountPath: "/etc/config-autoscaler",
						}, {
							Name:      loggingConfigName,
							MountPath: "/etc/config-logging",
						}},
					}},
					ServiceAccountName: "autoscaler",
					Volumes:            []corev1.Volume{autoscalerConfigVolume, loggingConfigVolume},
				},
			},
		},
	}
}

// MakeServingAutoscalerService returns a service for the autoscaler of
// the given revision.
func MakeServingAutoscalerService(rev *v1alpha1.Revision) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            controller.GetRevisionAutoscalerName(rev),
			Namespace:       pkg.GetServingSystemNamespace(),
			Labels:          makeServingAutoScalerLabels(rev),
			Annotations:     resources.MakeAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(rev)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "autoscaler-port",
				Port:       int32(resources.AutoscalerPort),
				TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: resources.AutoscalerPort},
			}},
			Type: "NodePort",
			Selector: map[string]string{
				serving.AutoscalerLabelKey: controller.GetRevisionAutoscalerName(rev),
			},
		},
	}
}

// makeServingAutoScalerLabels constructs the labels we will apply to
// service and deployment specs for autoscaler.
func makeServingAutoScalerLabels(rev *v1alpha1.Revision) map[string]string {
	labels := resources.MakeLabels(rev)
	labels[serving.AutoscalerLabelKey] = controller.GetRevisionAutoscalerName(rev)
	return labels
}
