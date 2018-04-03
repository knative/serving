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

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Each Elafros pod gets 1 cpu.
	elaContainerCpu   = "400m"
	queueContainerCpu = "25m"
)

// MakeElaPodSpec creates a pod spec.
func MakeElaPodSpec(u *v1alpha1.Revision) *corev1.PodSpec {
	elaContainer := u.Spec.Container.DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the validations in pkg/webhook.validateContainer.
	elaContainer.Name = elaContainerName
	elaContainer.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName("cpu"): resource.MustParse(elaContainerCpu),
		},
	}
	elaContainer.Ports = []corev1.ContainerPort{{
		Name:          elaPortName,
		ContainerPort: int32(elaPort),
	}}

	queueContainer := corev1.Container{
		Name:  queueContainerName,
		Image: queueSidecarImage,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse(queueContainerCpu),
			},
		},
		Ports: []corev1.ContainerPort{
			// TOOD: HTTPS connections from the Cloud LB require
			// certs. Right now, the static nginx.conf file has
			// been modified to only allow HTTP connections.
			{
				Name:          requestQueuePortName,
				ContainerPort: int32(requestQueuePort),
			},
		},
		Args: []string{
			"-logtostderr=true",
			"-stderrthreshold=INFO",
		},
		Env: []corev1.EnvVar{
			{
				Name:  "ELA_NAMESPACE",
				Value: u.Namespace,
			},
			{
				Name:  "ELA_REVISION",
				Value: u.Name,
			},
			{
				Name:  "ELA_AUTOSCALER",
				Value: controller.GetRevisionAutoscalerName(u),
			},
			{
				Name:  "ELA_AUTOSCALER_PORT",
				Value: strconv.Itoa(autoscalerPort),
			},
			{
				Name: "ELA_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
		},
	}

	return &corev1.PodSpec{
		Containers: []corev1.Container{*elaContainer, queueContainer},
	}
}

// MakeElaDeployment creates a deployment.
func MakeElaDeployment(u *v1alpha1.Revision, namespace string) *v1beta1.Deployment {
	rollingUpdateConfig := v1beta1.RollingUpdateDeployment{
		MaxUnavailable: &elaPodMaxUnavailable,
		MaxSurge:       &elaPodMaxSurge,
	}

	podTemplateAnnotations := MakeElaResourceAnnotations(u)
	podTemplateAnnotations[sidecarIstioInjectAnnotation] = "true"

	return &v1beta1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetRevisionDeploymentName(u),
			Namespace:   namespace,
			Labels:      MakeElaResourceLabels(u),
			Annotations: MakeElaResourceAnnotations(u),
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &elaPodReplicaCount,
			Strategy: v1beta1.DeploymentStrategy{
				Type:          "RollingUpdate",
				RollingUpdate: &rollingUpdateConfig,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: meta_v1.ObjectMeta{
					Labels:      MakeElaResourceLabels(u),
					Annotations: podTemplateAnnotations,
				},
			},
		},
	}
}
