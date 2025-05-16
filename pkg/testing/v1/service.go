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

package v1

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/domains"
	servicenames "knative.dev/serving/pkg/reconciler/service/resources/names"
)

// ServiceOption enables further configuration of a Service.
type ServiceOption func(*v1.Service)

// Service creates a service with ServiceOptions
func Service(name, namespace string, so ...ServiceOption) *v1.Service {
	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "cccccccc-cccc-cccc-cccc-cccccccccccc",
		},
	}
	for _, opt := range so {
		opt(s)
	}
	return s
}

// DefaultService creates a service with ServiceOptions and with default values set
func DefaultService(name, namespace string, so ...ServiceOption) *v1.Service {
	return Service(name, namespace, append(so, WithServiceDefaults)...)
}

// ServiceWithoutNamespace creates a service with ServiceOptions but without a specific namespace
func ServiceWithoutNamespace(name string, so ...ServiceOption) *v1.Service {
	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	for _, opt := range so {
		opt(s)
	}
	return s
}

// WithInitSvcConditions initializes the Service's conditions.
func WithInitSvcConditions(s *v1.Service) {
	s.Status.InitializeConditions()
}

// WithServiceObservedGenFailure marks the top level condition as unknown when the reconciler
// does not set any condition during reconciliation of a new generation.
func WithServiceObservedGenFailure(s *v1.Service) {
	condSet := s.GetConditionSet()
	condSet.Manage(&s.Status).MarkUnknown(condSet.GetTopLevelConditionType(),
		"NewObservedGenFailure", "unsuccessfully observed a new generation")
}

// WithConfigSpec configures the Service to use the given config spec
func WithConfigSpec(config *v1.ConfigurationSpec) ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.ConfigurationSpec = *config
	}
}

// WithRouteSpec configures the Service to use the given route spec
func WithRouteSpec(route v1.RouteSpec) ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.RouteSpec = route
	}
}

// WithNamedPort sets the name on the Service's port to the provided name
func WithNamedPort(name string) ServiceOption {
	return func(svc *v1.Service) {
		c := &svc.Spec.Template.Spec.Containers[0]
		if len(c.Ports) == 1 {
			c.Ports[0].Name = name
		} else {
			c.Ports = []corev1.ContainerPort{{
				Name: name,
			}}
		}
	}
}

// WithNumberedPort sets the Service's port number to what's provided.
func WithNumberedPort(number int32) ServiceOption {
	return func(svc *v1.Service) {
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

// WithServiceDefaults will set the default values on the service.
func WithServiceDefaults(svc *v1.Service) {
	svc.SetDefaults(context.Background())
}

// WithResourceRequirements attaches resource requirements to the service
func WithResourceRequirements(resourceRequirements corev1.ResourceRequirements) ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers[0].Resources = resourceRequirements
	}
}

// WithServiceAnnotation adds the given annotation to the service.
func WithServiceAnnotation(k, v string) ServiceOption {
	return func(svc *v1.Service) {
		svc.Annotations = kmeta.UnionMaps(svc.Annotations, map[string]string{
			k: v,
		})
	}
}

// WithServiceAnnotationRemoved removes the given annotation from the service.
func WithServiceAnnotationRemoved(k string) ServiceOption {
	return func(svc *v1.Service) {
		svc.Annotations = kmeta.FilterMap(svc.Annotations, func(s string) bool {
			return k == s
		})
	}
}

// WithServiceLabelRemoved removes the given label from the service.
func WithServiceLabelRemoved(k string) ServiceOption {
	return func(svc *v1.Service) {
		svc.Labels = kmeta.FilterMap(svc.Labels, func(s string) bool {
			return k == s
		})
	}
}

// WithServiceImage sets the container image to be the provided string.
func WithServiceImage(img string) ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers[0].Image = img
	}
}

// WithServiceName sets the service name.
func WithServiceName(name string) ServiceOption {
	return func(svc *v1.Service) {
		svc.ObjectMeta.Name = name
	}
}

// WithTrafficTarget sets the traffic to be the provided traffic target.
func WithTrafficTarget(tt []v1.TrafficTarget) ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Traffic = tt
	}
}

// WithServiceTemplateMeta sets the container image to be the provided string.
func WithServiceTemplateMeta(m metav1.ObjectMeta) ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.ObjectMeta = m
	}
}

// WithRevisionTimeoutSeconds sets revision timeout
func WithRevisionTimeoutSeconds(revisionTimeoutSeconds int64) ServiceOption {
	return func(service *v1.Service) {
		service.Spec.Template.Spec.TimeoutSeconds = ptr.Int64(revisionTimeoutSeconds)
	}
}

// WithRevisionResponseStartTimeoutSeconds sets revision first byte timeout
func WithRevisionResponseStartTimeoutSeconds(revisionResponseStartTimeoutSeconds int64) ServiceOption {
	return func(service *v1.Service) {
		service.Spec.Template.Spec.ResponseStartTimeoutSeconds = ptr.Int64(revisionResponseStartTimeoutSeconds)
	}
}

// WithRevisionIdleTimeoutSeconds sets revision idle timeout
func WithRevisionIdleTimeoutSeconds(revisionIdleTimeoutSeconds int64) ServiceOption {
	return func(service *v1.Service) {
		service.Spec.Template.Spec.IdleTimeoutSeconds = ptr.Int64(revisionIdleTimeoutSeconds)
	}
}

// WithServiceAccountName sets revision service account name
func WithServiceAccountName(serviceAccountName string) ServiceOption {
	return func(service *v1.Service) {
		service.Spec.Template.Spec.ServiceAccountName = serviceAccountName
	}
}

// WithVolume adds a volume to the service
func WithVolume(name, mountPath string, volumeSource corev1.VolumeSource) ServiceOption {
	return func(svc *v1.Service) {
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
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].Env = evs
	}
}

// WithEnvFrom configures the Service to use the provided environment variables.
func WithEnvFrom(evs ...corev1.EnvFromSource) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].EnvFrom = evs
	}
}

// WithSecurityContext configures the Service to use the provided security context.
func WithSecurityContext(sc *corev1.SecurityContext) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].SecurityContext = sc
	}
}

// WithWorkingDir configures the Service to use the provided working directory.
func WithWorkingDir(wd string) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].WorkingDir = wd
	}
}

// WithServiceAnnotations adds the supplied annotations to the Service
func WithServiceAnnotations(annotations map[string]string) ServiceOption {
	return func(service *v1.Service) {
		service.Annotations = kmeta.UnionMaps(service.Annotations, annotations)
	}
}

// WithConfigAnnotations assigns config annotations to a service
func WithConfigAnnotations(annotations map[string]string) ServiceOption {
	return func(service *v1.Service) {
		service.Spec.Template.Annotations = kmeta.UnionMaps(
			service.Spec.Template.Annotations, annotations)
	}
}

// WithBYORevisionName sets the given name to the config spec
func WithBYORevisionName(name string) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Name = name
	}
}

// WithServiceDeletionTimestamp will set the DeletionTimestamp on the Service.
func WithServiceDeletionTimestamp(r *v1.Service) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithNamedRevision configures the Service to use BYO Revision in the
// template spec and reference that same revision name in the route spec.
func WithNamedRevision(s *v1.Service) {
	s.Spec = v1.ServiceSpec{
		ConfigurationSpec: v1.ConfigurationSpec{
			Template: v1.RevisionTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: s.Name + "-byo",
				},
				Spec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Image: "busybox",
						}},
					},
					TimeoutSeconds: ptr.Int64(60),
				},
			},
		},
		RouteSpec: v1.RouteSpec{
			Traffic: []v1.TrafficTarget{{
				RevisionName: s.Name + "-byo",
				Percent:      ptr.Int64(100),
			}},
		},
	}
}

// WithOutOfDateConfig reflects the Configuration's readiness in the Service
// resource.
func WithOutOfDateConfig(s *v1.Service) {
	s.Status.MarkConfigurationNotReconciled()
}

// WithServiceGeneration sets the service's generation
func WithServiceGeneration(generation int64) ServiceOption {
	return func(svc *v1.Service) {
		svc.Status.ObservedGeneration = generation
	}
}

// WithServiceLabel attaches a particular label to the service.
func WithServiceLabel(key, value string) ServiceOption {
	return func(service *v1.Service) {
		if service.Labels == nil {
			service.Labels = make(map[string]string, 1)
		}
		service.Labels[key] = value
	}
}

// WithServiceObservedGeneration sets the service's observed generation to it's generation
func WithServiceObservedGeneration(svc *v1.Service) {
	svc.Status.ObservedGeneration = svc.Generation
}

// MarkRevisionNameTaken calls the function of the same name on the Service's status
func MarkRevisionNameTaken(service *v1.Service) {
	service.Status.MarkRevisionNameTaken(service.Spec.GetTemplate().GetName())
}

// WithRunLatestRollout configures the Service to use a "runLatest" rollout.
func WithRunLatestRollout(s *v1.Service) {
	s.Spec = v1.ServiceSpec{
		ConfigurationSpec: *configSpec.DeepCopy(),
	}
}

// WithReadyRoute reflects the Route's readiness in the Service resource.
func WithReadyRoute(s *v1.Service) {
	s.Status.PropagateRouteStatus(&v1.RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   "Ready",
				Status: "True",
			}},
		},
	})
}

// WithReadyConfig reflects the Configuration's readiness in the Service
// resource.  This must coincide with the setting of Latest{Created,Ready}
// to the provided revision name.
func WithReadyConfig(name string) ServiceOption {
	return func(s *v1.Service) {
		s.Status.PropagateConfigurationStatus(&v1.ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "Ready",
					Status: "True",
				}},
			},
			ConfigurationStatusFields: v1.ConfigurationStatusFields{
				LatestCreatedRevisionName: name,
				LatestReadyRevisionName:   name,
			},
		})
	}
}

// WithSvcStatusDomain propagates the domain name to the status of the Service.
func WithSvcStatusDomain(s *v1.Service) {
	n, ns := s.GetName(), s.GetNamespace()
	s.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.example.com", n, ns),
	}
}

// WithSvcStatusAddress updates the service's status with the address.
func WithSvcStatusAddress(s *v1.Service) {
	s.Status.Address = &duckv1.Addressable{
		URL: &apis.URL{
			Scheme: "http",
			Host:   network.GetServiceHostname(s.Name, s.Namespace),
		},
	}
}

// WithSvcStatusTraffic sets the Service's status traffic block to the specified traffic targets.
func WithSvcStatusTraffic(targets ...v1.TrafficTarget) ServiceOption {
	return func(r *v1.Service) {
		// Automatically inject URL into TrafficTarget status
		for _, tt := range targets {
			tt.URL = domains.URL(domains.HTTPScheme, tt.Tag+".example.com")
		}
		r.Status.Traffic = targets
	}
}

// WithServiceLatestReadyRevision sets the latest ready revision on the Service's status.
func WithServiceLatestReadyRevision(lrr string) ServiceOption {
	return func(s *v1.Service) {
		s.Status.LatestReadyRevisionName = lrr
	}
}

// WithReadinessProbe sets the provided probe to be the readiness
// probe on the service.
func WithReadinessProbe(p *corev1.Probe) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].ReadinessProbe = p
	}
}

// WithLivenessProbe sets the provided probe to be the liveness
// probe on the service.
func WithLivenessProbe(p *corev1.Probe) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].LivenessProbe = p
	}
}

// WithStartupProbe sets the provided probe to be the startup
// probe on the service.
func WithStartupProbe(p *corev1.Probe) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].StartupProbe = p
	}
}

// MarkConfigurationNotReconciled calls the function of the same name on the Service's status.
func MarkConfigurationNotReconciled(service *v1.Service) {
	service.Status.MarkConfigurationNotReconciled()
}

// WithServiceStatusRouteNotReady sets the `RoutesReady` condition on the service to `Unknown`.
func WithServiceStatusRouteNotReady(s *v1.Service) {
	s.Status.MarkRouteNotYetReady()
}

// MarkConfigurationNotOwned calls the function of the same name on the Service's status.
func MarkConfigurationNotOwned(service *v1.Service) {
	service.Status.MarkConfigurationNotOwned(servicenames.Configuration(service))
}

// MarkRouteNotOwned calls the function of the same name on the Service's status.
func MarkRouteNotOwned(service *v1.Service) {
	service.Status.MarkRouteNotOwned(servicenames.Route(service))
}

// WithFailedRoute reflects a Route's failure in the Service resource.
func WithFailedRoute(reason, message string) ServiceOption {
	return func(s *v1.Service) {
		s.Status.PropagateRouteStatus(&v1.RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:    "Ready",
					Status:  "False",
					Reason:  reason,
					Message: message,
				}},
			},
		})
	}
}

// WithFailedConfig reflects the Configuration's failure in the Service
// resource.  The failing revision's name is reflected in LatestCreated.
func WithFailedConfig(name, reason, message string) ServiceOption {
	return func(s *v1.Service) {
		s.Status.PropagateConfigurationStatus(&v1.ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "Ready",
					Status: "False",
					Reason: reason,
					Message: fmt.Sprintf("Revision %q failed with message: %s.",
						name, message),
				}},
			},
			ConfigurationStatusFields: v1.ConfigurationStatusFields{
				LatestCreatedRevisionName: name,
			},
		})
	}
}

// configSpec is the spec used for the different styles of Service rollout.
var configSpec = v1.ConfigurationSpec{
	Template: v1.RevisionTemplateSpec{
		Spec: v1.RevisionSpec{
			TimeoutSeconds: ptr.Int64(60),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "busybox",
				}},
			},
		},
	},
}

// WithInitContainer adds init container to a service.
func WithInitContainer(p corev1.Container) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.InitContainers = []corev1.Container{p}
	}
}

// WithPodSecurityContext assigns security context to a service.
func WithPodSecurityContext(secCtx corev1.PodSecurityContext) ServiceOption {
	return func(s *v1.Service) {
		s.Spec.Template.Spec.SecurityContext = &secCtx
	}
}

func WithNamespace(namespace string) ServiceOption {
	return func(svc *v1.Service) {
		svc.ObjectMeta.Namespace = namespace
	}
}
