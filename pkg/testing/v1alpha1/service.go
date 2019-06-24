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

package v1alpha1

import (
	"context"
	"fmt"
	"time"

	"github.com/knative/pkg/apis"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/reconciler/route/domains"
	servicenames "github.com/knative/serving/pkg/reconciler/service/resources/names"
	"github.com/knative/serving/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceOption enables further configuration of a Service.
type ServiceOption func(*v1alpha1.Service)

// Service creates a service with ServiceOptions
func Service(name, namespace string, so ...ServiceOption) *v1alpha1.Service {
	s := &v1alpha1.Service{
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
func ServiceWithoutNamespace(name string, so ...ServiceOption) *v1alpha1.Service {
	s := &v1alpha1.Service{
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

// WithServiceDeletionTimestamp will set the DeletionTimestamp on the Service.
func WithServiceDeletionTimestamp(r *v1alpha1.Service) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithEnv configures the Service to use the provided environment variables.
func WithEnv(evs ...corev1.EnvVar) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec.Template.Spec.Containers[0].Env = evs
	}
}

// WithEnvFrom configures the Service to use the provided environment variables.
func WithEnvFrom(evs ...corev1.EnvFromSource) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec.Template.Spec.Containers[0].EnvFrom = evs
	}
}

// WithInlineRollout configures the Service to be "run latest" via inline
// Route/Configuration
func WithInlineRollout(s *v1alpha1.Service) {
	s.Spec = v1alpha1.ServiceSpec{
		ConfigurationSpec: v1alpha1.ConfigurationSpec{
			Template: &v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					RevisionSpec: v1beta1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
						TimeoutSeconds: ptr.Int64(60),
					},
				},
			},
		},
		RouteSpec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{{
				TrafficTarget: v1beta1.TrafficTarget{
					Percent: 100,
				},
			}},
		},
	}
}

// WithRunLatestRollout configures the Service to use a "runLatest" rollout.
func WithRunLatestRollout(s *v1alpha1.Service) {
	s.Spec = v1alpha1.ServiceSpec{
		DeprecatedRunLatest: &v1alpha1.RunLatestType{
			Configuration: *configSpec.DeepCopy(),
		},
	}
}

// WithInlineConfigSpec confgures the Service to use the given config spec
func WithInlineConfigSpec(config v1alpha1.ConfigurationSpec) ServiceOption {
	return func(svc *v1alpha1.Service) {
		svc.Spec.ConfigurationSpec = config
	}
}

// WithInlineRouteSpec confgures the Service to use the given route spec
func WithInlineRouteSpec(config v1alpha1.RouteSpec) ServiceOption {
	return func(svc *v1alpha1.Service) {
		svc.Spec.RouteSpec = config
	}
}

// WithRunLatestConfigSpec confgures the Service to use a "runLatest" configuration
func WithRunLatestConfigSpec(config v1alpha1.ConfigurationSpec) ServiceOption {
	return func(svc *v1alpha1.Service) {
		svc.Spec = v1alpha1.ServiceSpec{
			DeprecatedRunLatest: &v1alpha1.RunLatestType{
				Configuration: config,
			},
		}
	}
}

// WithServiceLabel attaches a particular label to the service.
func WithServiceLabel(key, value string) ServiceOption {
	return func(service *v1alpha1.Service) {
		if service.Labels == nil {
			service.Labels = make(map[string]string)
		}
		service.Labels[key] = value
	}
}

// WithNumberedPort sets the Service's port number to what's provided.
func WithNumberedPort(number int32) ServiceOption {
	return func(svc *v1alpha1.Service) {
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
	return func(svc *v1alpha1.Service) {
		if svc.Spec.DeprecatedRunLatest != nil {
			svc.Spec.DeprecatedRunLatest.Configuration.GetTemplate().Spec.GetContainer().Resources = resourceRequirements
		} else {
			svc.Spec.ConfigurationSpec.Template.Spec.Containers[0].Resources = resourceRequirements
		}
	}
}

// WithVolume adds a volume to the service
func WithVolume(name, mountPath string, volumeSource corev1.VolumeSource) ServiceOption {
	return func(svc *v1alpha1.Service) {
		var rt *v1alpha1.RevisionSpec
		if svc.Spec.DeprecatedRunLatest != nil {
			rt = &svc.Spec.DeprecatedRunLatest.Configuration.GetTemplate().Spec
		} else {
			rt = &svc.Spec.ConfigurationSpec.Template.Spec
		}

		rt.GetContainer().VolumeMounts = append(rt.GetContainer().VolumeMounts,
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

func WithServiceAnnotations(annotations map[string]string) ServiceOption {
	return func(service *v1alpha1.Service) {
		service.Annotations = resources.UnionMaps(service.Annotations, annotations)
	}
}

// WithConfigAnnotations assigns config annotations to a service
func WithConfigAnnotations(annotations map[string]string) ServiceOption {
	return func(service *v1alpha1.Service) {
		if service.Spec.DeprecatedRunLatest != nil {
			service.Spec.DeprecatedRunLatest.Configuration.GetTemplate().ObjectMeta.Annotations = annotations
		} else {
			service.Spec.ConfigurationSpec.Template.ObjectMeta.Annotations = annotations
		}
	}
}

// WithRevisionTimeoutSeconds sets revision timeout
func WithRevisionTimeoutSeconds(revisionTimeoutSeconds int64) ServiceOption {
	return func(service *v1alpha1.Service) {
		if service.Spec.DeprecatedRunLatest != nil {
			service.Spec.DeprecatedRunLatest.Configuration.GetTemplate().Spec.TimeoutSeconds = ptr.Int64(revisionTimeoutSeconds)
		} else {
			service.Spec.ConfigurationSpec.Template.Spec.TimeoutSeconds = ptr.Int64(revisionTimeoutSeconds)
		}
	}
}

// MarkConfigurationNotReconciled calls the function of the same name on the Service's status.
func MarkConfigurationNotReconciled(service *v1alpha1.Service) {
	service.Status.MarkConfigurationNotReconciled()
}

// MarkConfigurationNotOwned calls the function of the same name on the Service's status.
func MarkConfigurationNotOwned(service *v1alpha1.Service) {
	service.Status.MarkConfigurationNotOwned(servicenames.Configuration(service))
}

// MarkRouteNotOwned calls the function of the same name on the Service's status.
func MarkRouteNotOwned(service *v1alpha1.Service) {
	service.Status.MarkRouteNotOwned(servicenames.Route(service))
}

// WithPinnedRollout configures the Service to use a "pinned" rollout,
// which is pinned to the named revision.
// Deprecated, since PinnedType is deprecated.
func WithPinnedRollout(name string) ServiceOption {
	return WithPinnedRolloutConfigSpec(name, *configSpec.DeepCopy())
}

// WithPinnedRolloutConfigSpec WithPinnedRollout2
func WithPinnedRolloutConfigSpec(name string, config v1alpha1.ConfigurationSpec) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec = v1alpha1.ServiceSpec{
			DeprecatedPinned: &v1alpha1.PinnedType{
				RevisionName:  name,
				Configuration: config,
			},
		}
	}
}

// WithReleaseRolloutAndPercentage configures the Service to use a "release" rollout,
// which spans the provided revisions.
func WithReleaseRolloutAndPercentage(percentage int, names ...string) ServiceOption {
	return WithReleaseRolloutAndPercentageConfigSpec(percentage, *configSpec.DeepCopy(),
		names...)
}

// WithReleaseRolloutAndPercentageConfigSpec configures the Service to use a "release" rollout,
// which spans the provided revisions.
func WithReleaseRolloutAndPercentageConfigSpec(percentage int, config v1alpha1.ConfigurationSpec, names ...string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec = v1alpha1.ServiceSpec{
			DeprecatedRelease: &v1alpha1.ReleaseType{
				Revisions:      names,
				RolloutPercent: percentage,
				Configuration:  config,
			},
		}
	}
}

// WithReleaseRolloutConfigSpec configures the Service to use a "release" rollout,
// which spans the provided revisions.
func WithReleaseRolloutConfigSpec(config v1alpha1.ConfigurationSpec, names ...string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec = v1alpha1.ServiceSpec{
			DeprecatedRelease: &v1alpha1.ReleaseType{
				Revisions:     names,
				Configuration: config,
			},
		}
	}
}

// WithReleaseRollout configures the Service to use a "release" rollout,
// which spans the provided revisions.
func WithReleaseRollout(names ...string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec = v1alpha1.ServiceSpec{
			DeprecatedRelease: &v1alpha1.ReleaseType{
				Revisions:     names,
				Configuration: *configSpec.DeepCopy(),
			},
		}
	}
}

// WithInitSvcConditions initializes the Service's conditions.
func WithInitSvcConditions(s *v1alpha1.Service) {
	s.Status.InitializeConditions()
}

// WithReadyRoute reflects the Route's readiness in the Service resource.
func WithReadyRoute(s *v1alpha1.Service) {
	s.Status.PropagateRouteStatus(&v1alpha1.RouteStatus{
		Status: duckv1beta1.Status{
			Conditions: duckv1beta1.Conditions{{
				Type:   "Ready",
				Status: "True",
			}},
		},
	})
}

// WithSvcStatusDomain propagates the domain name to the status of the Service.
func WithSvcStatusDomain(s *v1alpha1.Service) {
	n, ns := s.GetName(), s.GetNamespace()
	s.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.example.com", n, ns),
	}
}

// WithSvcStatusAddress updates the service's status with the address.
func WithSvcStatusAddress(s *v1alpha1.Service) {
	s.Status.Address = &duckv1alpha1.Addressable{
		Addressable: duckv1beta1.Addressable{
			URL: &apis.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("%s.%s.svc.cluster.local", s.Name, s.Namespace),
			},
		},
	}
}

// WithSvcStatusTraffic sets the Service's status traffic block to the specified traffic targets.
func WithSvcStatusTraffic(targets ...v1alpha1.TrafficTarget) ServiceOption {
	return func(r *v1alpha1.Service) {
		// Automatically inject URL into TrafficTarget status
		for _, tt := range targets {
			tt.URL = domains.URL(domains.HTTPScheme, tt.Tag+".example.com")
		}
		r.Status.Traffic = targets
	}
}

// WithFailedRoute reflects a Route's failure in the Service resource.
func WithFailedRoute(reason, message string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Status.PropagateRouteStatus(&v1alpha1.RouteStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:    "Ready",
					Status:  "False",
					Reason:  reason,
					Message: message,
				}},
			},
		})
	}
}

// WithReadyConfig reflects the Configuration's readiness in the Service
// resource.  This must coincide with the setting of Latest{Created,Ready}
// to the provided revision name.
func WithReadyConfig(name string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Status.PropagateConfigurationStatus(&v1alpha1.ConfigurationStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   "Ready",
					Status: "True",
				}},
			},
			ConfigurationStatusFields: v1alpha1.ConfigurationStatusFields{
				LatestCreatedRevisionName: name,
				LatestReadyRevisionName:   name,
			},
		})
	}
}

// WithFailedConfig reflects the Configuration's failure in the Service
// resource.  The failing revision's name is reflected in LatestCreated.
func WithFailedConfig(name, reason, message string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Status.PropagateConfigurationStatus(&v1alpha1.ConfigurationStatus{
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   "Ready",
					Status: "False",
					Reason: reason,
					Message: fmt.Sprintf("Revision %q failed with message: %s.",
						name, message),
				}},
			},
			ConfigurationStatusFields: v1alpha1.ConfigurationStatusFields{
				LatestCreatedRevisionName: name,
			},
		})
	}
}

// WithServiceLatestReadyRevision sets the latest ready revision on the Service's status.
func WithServiceLatestReadyRevision(lrr string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Status.LatestReadyRevisionName = lrr
	}
}

// WithServiceStatusRouteNotReady sets the `RoutesReady` condition on the service to `Unknown`.
func WithServiceStatusRouteNotReady(s *v1alpha1.Service) {
	s.Status.MarkRouteNotYetReady()
}

// WithSecurityContext configures the Service to use the provided security context.
func WithSecurityContext(sc *corev1.SecurityContext) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec.Template.Spec.Containers[0].SecurityContext = sc
	}
}

// WithWorkingDir configures the Service to use the provided working directory.
func WithWorkingDir(wd string) ServiceOption {
	return func(s *v1alpha1.Service) {
		s.Spec.Template.Spec.Containers[0].WorkingDir = wd
	}
}
