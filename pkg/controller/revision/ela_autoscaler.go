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
	"strconv"

	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
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
func MakeElaAutoscalerDeployment(rev *v1alpha1.Revision, autoscalerImage string) *v1beta1.Deployment {
	rollingUpdateConfig := v1beta1.RollingUpdateDeployment{
		MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
		MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
	}

	labels := MakeElaResourceLabels(rev)
	labels[ela.AutoscalerLabelKey] = controller.GetRevisionAutoscalerName(rev)
	annotations := MakeElaResourceAnnotations(rev)
	annotations[sidecarIstioInjectAnnotation] = "false"

	replicas := int32(1)
	return &v1beta1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetRevisionAutoscalerName(rev),
			Namespace:   AutoscalerNamespace,
			Labels:      MakeElaResourceLabels(rev),
			Annotations: MakeElaResourceAnnotations(rev),
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: v1beta1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels:      labels,
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
							},
						},
					},
					ServiceAccountName: "ela-autoscaler",
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
			Labels:      MakeElaResourceLabels(rev),
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
				ela.AutoscalerLabelKey: controller.GetRevisionAutoscalerName(rev),
			},
		},
	}
}
