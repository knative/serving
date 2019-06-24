/*
Copyright 2019 The Knative Authors

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

package v1beta1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	presources "github.com/knative/serving/pkg/resources"
)

// ServiceOption enables further configuration of a Service.
type ServiceOption func(*v1beta1.Service)

// Service creates a service with ServiceOptions
func Service(name, namespace string, so ...ServiceOption) *v1beta1.Service {
	s := &v1beta1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	for _, opt := range so {
		opt(s)
	}
	s.SetDefaults(context.Background())
	return s
}

// ServiceWithoutNamespace creates a service with ServiceOptions but without a specific namespace
func ServiceWithoutNamespace(name string, so ...ServiceOption) *v1beta1.Service {
	s := &v1beta1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	for _, opt := range so {
		opt(s)
	}
	s.SetDefaults(context.Background())
	return s
}

// WithInlineConfigSpec confgures the Service to use the given config spec
func WithInlineConfigSpec(config v1beta1.ConfigurationSpec) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Spec.ConfigurationSpec = config
	}
}

// WithNamedPort sets the name on the Service's port to the provided name
func WithNamedPort(name string) ServiceOption {
	return func(svc *v1beta1.Service) {
		c := &svc.Spec.Template.Spec.Containers[0]
		if len(c.Ports) == 1 {
			c.Ports[0].Name = name
			c.Ports[0].ContainerPort = 8080
		} else {
			c.Ports = []corev1.ContainerPort{{
				Name:          name,
				ContainerPort: 8080,
			}}
		}
	}
}

// WithNumberedPort sets the Service's port number to what's provided.
func WithNumberedPort(number int32) ServiceOption {
	return func(svc *v1beta1.Service) {
		c := &svc.Spec.Template.Spec.Containers[0]
		if len(c.Ports) == 1 {
			c.Ports[0].ContainerPort = number
		} else {
			c.Ports = []corev1.ContainerPort{{
				ContainerPort: number,
			}}
		}
	}
}

// WithResourceRequirements attaches resource requirements to the service
func WithResourceRequirements(resourceRequirements corev1.ResourceRequirements) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Spec.Template.Spec.Containers[0].Resources = resourceRequirements
	}
}

// WithServiceAnnotation adds the given annotation to the service.
func WithServiceAnnotation(k, v string) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Annotations = presources.UnionMaps(svc.Annotations, map[string]string{
			k: v,
		})
	}
}

// WithServiceAnnotationRemoved adds the given annotation to the service.
func WithServiceAnnotationRemoved(k string) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Annotations = presources.FilterMap(svc.Annotations, func(s string) bool {
			return k == s
		})
	}
}

// WithServiceImage sets the container image to be the provided string.
func WithServiceImage(img string) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Spec.Template.Spec.Containers[0].Image = img
	}
}

// WithServiceTemplateMeta sets the container image to be the provided string.
func WithServiceTemplateMeta(m metav1.ObjectMeta) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Spec.Template.ObjectMeta = m
	}
}

// WithRevisionTimeoutSeconds sets revision timeout
func WithRevisionTimeoutSeconds(revisionTimeoutSeconds int64) ServiceOption {
	return func(service *v1beta1.Service) {
		service.Spec.Template.Spec.TimeoutSeconds = ptr.Int64(revisionTimeoutSeconds)
	}
}

// WithContainerConcurrency sets the given Service's concurrency.
func WithContainerConcurrency(cc v1beta1.RevisionContainerConcurrencyType) ServiceOption {
	return func(svc *v1beta1.Service) {
		svc.Spec.Template.Spec.ContainerConcurrency = cc
	}
}

// WithVolume adds a volume to the service
func WithVolume(name, mountPath string, volumeSource corev1.VolumeSource) ServiceOption {
	return func(svc *v1beta1.Service) {
		rt := &svc.Spec.ConfigurationSpec.Template.Spec

		rt.Containers[0].VolumeMounts = append(rt.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      name,
				MountPath: mountPath,
			})

		rt.Volumes = append(rt.Volumes, corev1.Volume{
			Name:         name,
			VolumeSource: volumeSource,
		})
	}
}

// WithEnv configures the Service to use the provided environment variables.
func WithEnv(evs ...corev1.EnvVar) ServiceOption {
	return func(s *v1beta1.Service) {
		s.Spec.Template.Spec.Containers[0].Env = evs
	}
}

// WithEnvFrom configures the Service to use the provided environment variables.
func WithEnvFrom(evs ...corev1.EnvFromSource) ServiceOption {
	return func(s *v1beta1.Service) {
		s.Spec.Template.Spec.Containers[0].EnvFrom = evs
	}
}

// WithSecurityContext configures the Service to use the provided security context.
func WithSecurityContext(sc *corev1.SecurityContext) ServiceOption {
	return func(s *v1beta1.Service) {
		s.Spec.Template.Spec.Containers[0].SecurityContext = sc
	}
}

// WithWorkingDir configures the Service to use the provided working directory.
func WithWorkingDir(wd string) ServiceOption {
	return func(s *v1beta1.Service) {
		s.Spec.Template.Spec.Containers[0].WorkingDir = wd
	}
}
