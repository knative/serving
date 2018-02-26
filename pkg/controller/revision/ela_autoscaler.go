/*
Copyright 2017 The Kubernetes Authors.

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
	"flag"
	"strconv"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/google/elafros/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var autoscalerImage string

func init() {
	flag.StringVar(&autoscalerImage, "autoscalerImage", "", "The digest of the autoscaler image.")
}

func MakeElaAutoscalerDeployment(u *v1alpha1.Revision, namespace string) *v1beta1.Deployment {
	rollingUpdateConfig := v1beta1.RollingUpdateDeployment{
		MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
		MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
	}
	replicas := int32(1)
	targetConcurrencyPerProcess := "10"
	if spec := u.Spec.Scaling; spec != nil {
		targetConcurrencyPerProcess = strconv.Itoa(int(spec.TargetConcurrencyPerProcess))
	}
	return &v1beta1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      controller.GetRevisionAutoscalerName(u),
			Namespace: namespace,
			Labels: map[string]string{
				"revision": u.Name,
			},
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: v1beta1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels: map[string]string{
						"autoscaler": controller.GetRevisionAutoscalerName(u),
					},
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
								ContainerPort: int32(8080),
							}},
							Env: []corev1.EnvVar{
								{
									Name:  "ELA_NAMESPACE",
									Value: u.Namespace,
								},
								{
									Name:  "ELA_DEPLOYMENT",
									Value: controller.GetRevisionDeploymentName(u),
								},
								{
									Name:  "ELA_TARGET_CONCURRENCY",
									Value: targetConcurrencyPerProcess,
								},
							},
						},
					},
					ServiceAccountName: "ela-revision", // TODO(josephburnett) create ela-autoscaler service account
				},
			},
		},
	}
}

func MakeElaAutoscalerService(u *v1alpha1.Revision, namespace string) *corev1.Service {
	name := u.Name
	serviceID := u.Spec.Service
	return &corev1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      controller.GetRevisionAutoscalerName(u),
			Namespace: namespace,
			Labels: map[string]string{
				routeLabel:      serviceID,
				elaVersionLabel: name,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "autoscaler-port",
					Port:       int32(8080),
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			Type: "NodePort",
			Selector: map[string]string{
				"autoscaler": controller.GetRevisionAutoscalerName(u),
			},
		},
	}
}
