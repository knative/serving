/*
Copyright 2018 Google LLC

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

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// AutoscalerNamespace needs to match the service account, which needs to
// be a single, known namespace. This ensures that projects created in
// non-default namespaces continue to work with autoscaling.
const AutoscalerNamespace = "ela-system"

// MakeElaAutoscalerDeployment creates the deployment of the
// autoscaler for a particular revision.
func MakeElaAutoscalerDeployment(rev *v1alpha1.Revision, autoscalerImage string) *appsv1.Deployment {
	rollingUpdateConfig := appsv1.RollingUpdateDeployment{
		MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
		MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
	}

	annotations := MakeElaResourceAnnotations(rev)
	annotations[sidecarIstioInjectAnnotation] = "false"

	replicas := int32(1)

	configVolume := corev1.Volume{
		Name: "ela-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "ela-config",
				},
			},
		},
	}
	return &appsv1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetRevisionAutoscalerName(rev),
			Namespace:   AutoscalerNamespace,
			Labels:      MakeElaResourceLabels(rev),
			Annotations: MakeElaResourceAnnotations(rev),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: MakeElaResourceSelector(rev),
			Strategy: appsv1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels:      makeElaAutoScalerLabels(rev),
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name:  "autoscaler",
							Image: autoscalerImage,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName("cpu"): resource.MustParse("25m"),
								},
							},
							Ports: []corev1.ContainerPort{{
								Name:          "autoscaler-port",
								ContainerPort: autoscalerPort,
							}},
							Env: []corev1.EnvVar{
								{
									Name:  "ELA_NAMESPACE",
									Value: rev.Namespace,
								},
								{
									Name:  "ELA_DEPLOYMENT",
									Value: controller.GetRevisionDeploymentName(rev),
								},
								{
									Name:  "ELA_CONFIGURATION",
									Value: controller.LookupOwningConfigurationName(rev.OwnerReferences),
								},
								{
									Name:  "ELA_REVISION",
									Value: rev.Name,
								},
								{
									Name:  "ELA_AUTOSCALER_PORT",
									Value: strconv.Itoa(autoscalerPort),
								},
							},
							Args: []string{
								"-logtostderr=true",
								"-stderrthreshold=INFO",
								fmt.Sprintf("-concurrencyModel=%v", rev.Spec.ConcurrencyModel),
							},
							VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "ela-config",
									MountPath: "/etc/config",
								},
							},
						},
					},
					ServiceAccountName: "ela-autoscaler",
					Volumes:            []corev1.Volume{configVolume},
				},
			},
		},
	}
}

// MakeElaAutoscalerService returns a service for the autoscaler of
// the given revision.
func MakeElaAutoscalerService(rev *v1alpha1.Revision) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetRevisionAutoscalerName(rev),
			Namespace:   AutoscalerNamespace,
			Labels:      makeElaAutoScalerLabels(rev),
			Annotations: MakeElaResourceAnnotations(rev),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "autoscaler-port",
					Port:       int32(autoscalerPort),
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: autoscalerPort},
				},
			},
			Type: "NodePort",
			Selector: map[string]string{
				serving.AutoscalerLabelKey: controller.GetRevisionAutoscalerName(rev),
			},
		},
	}
}

// makeElaAutoScalerLabels constructs the labels we will apply to
// service and deployment specs for autoscaler.
func makeElaAutoScalerLabels(rev *v1alpha1.Revision) map[string]string {
	labels := MakeElaResourceLabels(rev)
	labels[serving.AutoscalerLabelKey] = controller.GetRevisionAutoscalerName(rev)
	return labels
}
