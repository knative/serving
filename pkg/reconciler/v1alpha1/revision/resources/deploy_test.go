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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	one            int32  = 1
	defaultPortStr string = strconv.Itoa(int(v1alpha1.DefaultUserPort))
)

func refInt64(num int64) *int64 {
	return &num
}

func TestMakePodSpec(t *testing.T) {
	labels := map[string]string{serving.ConfigurationLabelKey: "cfg", serving.ServiceLabelKey: "svc"}
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		lc   *logging.Config
		oc   *config.Observability
		ac   *autoscaler.Config
		cc   *config.Controller
		want *corev1.PodSpec
	}{{
		name: "user-defined user port, queue proxy have PORT env",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       45,
				Container: corev1.Container{
					Image: "busybox",
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8888,
						},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:      UserContainerName,
				Image:     "busybox",
				Resources: userResources,
				Ports: []corev1.ContainerPort{
					{
						Name:          v1alpha1.UserPortName,
						ContainerPort: 8888,
					},
				},
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: "8888", // match user port
				}, {
					Name:  "K_REVISION",
					Value: "bar",
				}, {
					Name:  "K_CONFIGURATION",
					Value: "cfg",
				}, {
					Name:  "K_SERVICE",
					Value: "svc",
				}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "1",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8888", // Match user port
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "simple concurrency=single no owner",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:                     UserContainerName,
				Image:                    "busybox",
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "1",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "simple concurrency=single no owner digest resolved",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
			Status: v1alpha1.RevisionStatus{
				ImageDigest: "busybox@sha256:deadbeef",
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:                     UserContainerName,
				Image:                    "busybox@sha256:deadbeef",
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "1",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "simple concurrency=single with owner",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "parent-config",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:                     UserContainerName,
				Image:                    "busybox",
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name:  "SERVING_CONFIGURATION",
					Value: "parent-config",
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "1",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "simple concurrency=multi http readiness probe",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(v1alpha1.DefaultUserPort),
								Path: "/",
							},
						},
					},
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  UserContainerName,
				Image: "busybox",
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Port: intstr.FromInt(v1alpha1.RequestQueuePort),
							Path: "/",
						},
					},
				},
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "0",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "concurrency=multi, readinessprobe=shell",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							Exec: &corev1.ExecAction{
								Command: []string{"echo", "hello"},
							},
						},
					},
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  UserContainerName,
				Image: "busybox",
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"echo", "hello"},
						},
					},
				},
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "0",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "concurrency=multi, readinessprobe=http",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/",
							},
						},
					},
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  UserContainerName,
				Image: "busybox",
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							// HTTP probes route through the queue
							Port: intstr.FromInt(v1alpha1.RequestQueuePort),
						},
					},
				},
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "0",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "concurrency=multi, livenessprobe=tcp",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
					LivenessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							TCPSocket: &corev1.TCPSocketAction{},
						},
					},
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  UserContainerName,
				Image: "busybox",
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(v1alpha1.DefaultUserPort),
						},
					},
				},
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "0",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "with /var/log collection",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{
			EnableVarLogCollection: true,
			FluentdSidecarImage:    "indiana:jones",
		},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:                     UserContainerName,
				Image:                    "busybox",
				Resources:                userResources,
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{buildUserPortEnv(defaultPortStr),
					{
						Name:  "K_REVISION",
						Value: "bar",
					}, {
						Name:  "K_CONFIGURATION",
						Value: "cfg",
					}, {
						Name:  "K_SERVICE",
						Value: "svc",
					}},
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "1",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}, {
				Name:      FluentdContainerName,
				Image:     "indiana:jones",
				Resources: fluentdResources,
				Env: []corev1.EnvVar{{
					Name:  "FLUENTD_ARGS",
					Value: "--no-supervisor -q",
				}, {
					Name:  "SERVING_CONTAINER_NAME",
					Value: UserContainerName,
				}, {
					Name: "SERVING_CONFIGURATION",
					// No owner reference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar",
				}, {
					Name:  "SERVING_NAMESPACE",
					Value: "foo",
				}, {
					Name: "SERVING_POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				}},
				VolumeMounts: fluentdVolumeMounts,
			}},
			Volumes: []corev1.Volume{varLogVolume, {
				Name: fluentdConfigMapVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "bar-fluentd",
						},
					},
				},
			}},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}, {
		name: "complex pod spec",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image:   "busybox",
					Command: []string{"/bin/bash"},
					Args:    []string{"-c", "echo Hello world"},
					Env: []corev1.EnvVar{{
						Name:  "FOO",
						Value: "bar",
					}, {
						Name:  "BAZ",
						Value: "blah",
					}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("666Mi"),
							corev1.ResourceCPU:    resource.MustParse("666m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("888Mi"),
							corev1.ResourceCPU:    resource.MustParse("888m"),
						},
					},
					TerminationMessagePolicy: corev1.TerminationMessageReadFile,
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:    UserContainerName,
				Image:   "busybox",
				Command: []string{"/bin/bash"},
				Args:    []string{"-c", "echo Hello world"},
				Env: []corev1.EnvVar{{
					Name:  "FOO",
					Value: "bar",
				}, {
					Name:  "BAZ",
					Value: "blah",
				}, {
					Name:  "PORT",
					Value: "8080",
				}, {
					Name:  "K_REVISION",
					Value: "bar",
				}, {
					Name:  "K_CONFIGURATION",
					Value: "",
				}, {
					Name:  "K_SERVICE",
					Value: "",
				}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("666Mi"),
						corev1.ResourceCPU:    resource.MustParse("666m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("888Mi"),
						corev1.ResourceCPU:    resource.MustParse("888m"),
					},
				},
				Ports:                    buildContainerPorts(v1alpha1.DefaultUserPort),
				VolumeMounts:             []corev1.VolumeMount{varLogVolumeMount},
				Lifecycle:                userLifecycle,
				TerminationMessagePolicy: corev1.TerminationMessageReadFile,
			}, {
				Name:           QueueContainerName,
				Resources:      queueResources,
				Ports:          queuePorts,
				Lifecycle:      queueLifecycle,
				ReadinessProbe: queueReadinessProbe,
				// These changed based on the Revision and configs passed in.
				Env: []corev1.EnvVar{{
					Name:  "SERVING_NAMESPACE",
					Value: "foo", // matches namespace
				}, {
					Name: "SERVING_CONFIGURATION",
					// No OwnerReference
				}, {
					Name:  "SERVING_REVISION",
					Value: "bar", // matches name
				}, {
					Name:  "SERVING_AUTOSCALER",
					Value: "autoscaler", // no autoscaler configured.
				}, {
					Name:  "SERVING_AUTOSCALER_PORT",
					Value: "8080",
				}, {
					Name:  "CONTAINER_CONCURRENCY",
					Value: "1",
				}, {
					Name:  "REVISION_TIMEOUT_SECONDS",
					Value: "45",
				}, {
					Name: "SERVING_POD",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}, {
					Name: "SERVING_LOGGING_CONFIG",
					// No logging configuration
				}, {
					Name: "SERVING_LOGGING_LEVEL",
					// No logging level
				}, {
					Name:  "USER_PORT",
					Value: "8080",
				}},
			}},
			Volumes:                       []corev1.Volume{varLogVolume},
			TerminationGracePeriodSeconds: refInt64(45),
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quantityComparer := cmp.Comparer(func(x, y resource.Quantity) bool {
				return x.Cmp(y) == 0
			})

			got := makePodSpec(test.rev, test.lc, test.oc, test.ac, test.cc)
			if diff := cmp.Diff(test.want, got, quantityComparer); diff != "" {
				t.Errorf("makePodSpec (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeDeployment(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		lc   *logging.Config
		nc   *config.Network
		oc   *config.Observability
		ac   *autoscaler.Config
		cc   *config.Controller
		want *appsv1.Deployment
	}{{
		name: "simple concurrency=single no owner",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 1,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		nc: &config.Network{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-deployment",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionUID: "1234",
					},
				},
				ProgressDeadlineSeconds: &ProgressDeadlineSeconds,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.RevisionLabelKey: "bar",
							serving.RevisionUID:      "1234",
							AppLabelKey:              "bar",
						},
						Annotations: map[string]string{
							sidecarIstioInjectAnnotation: "true",
						},
					},
					// Spec: filled in below by makePodSpec
				},
			},
		},
	}, {
		name: "simple concurrency=multi with owner",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "parent-config",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		nc: &config.Network{},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-deployment",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionUID: "1234",
					},
				},
				ProgressDeadlineSeconds: &ProgressDeadlineSeconds,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.RevisionLabelKey: "bar",
							serving.RevisionUID:      "1234",
							AppLabelKey:              "bar",
						},
						Annotations: map[string]string{
							sidecarIstioInjectAnnotation: "true",
						},
					},
					// Spec: filled in below by makePodSpec
				},
			},
		},
	}, {
		name: "simple concurrency=multi with outbound IP range configured",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		nc: &config.Network{
			IstioOutboundIPRanges: "*",
		},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-deployment",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionUID: "1234",
					},
				},
				ProgressDeadlineSeconds: &ProgressDeadlineSeconds,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.RevisionLabelKey: "bar",
							serving.RevisionUID:      "1234",
							AppLabelKey:              "bar",
						},
						Annotations: map[string]string{
							sidecarIstioInjectAnnotation:   "true",
							IstioOutboundIPRangeAnnotation: "*",
						},
					},
					// Spec: filled in below by makePodSpec
				},
			},
		},
	}, {
		name: "simple concurrency=multi with outbound IP range override",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Annotations: map[string]string{
					IstioOutboundIPRangeAnnotation: "10.4.0.0/14,10.7.240.0/20",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				ContainerConcurrency: 0,
				Container: corev1.Container{
					Image: "busybox",
				},
				TimeoutSeconds: 45,
			},
		},
		lc: &logging.Config{},
		nc: &config.Network{
			IstioOutboundIPRanges: "*",
		},
		oc: &config.Observability{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-deployment",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{
					IstioOutboundIPRangeAnnotation: "10.4.0.0/14,10.7.240.0/20",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						serving.RevisionUID: "1234",
					},
				},
				ProgressDeadlineSeconds: &ProgressDeadlineSeconds,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							serving.RevisionLabelKey: "bar",
							serving.RevisionUID:      "1234",
							AppLabelKey:              "bar",
						},
						Annotations: map[string]string{
							sidecarIstioInjectAnnotation: "true",
							// The annotation on the Revision should override our global configuration.
							IstioOutboundIPRangeAnnotation: "10.4.0.0/14,10.7.240.0/20",
						},
					},
					// Spec: filled in below by makePodSpec
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Tested above so that we can rely on it here for brevity.
			test.want.Spec.Template.Spec = *makePodSpec(test.rev, test.lc, test.oc, test.ac, test.cc)
			got := MakeDeployment(test.rev, test.lc, test.nc, test.oc, test.ac, test.cc)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("MakeDeployment (-want, +got) = %v", diff)
			}
		})
	}
}

func TestApplyDefaultResources(t *testing.T) {
	tests := []struct {
		name     string
		defaults corev1.ResourceRequirements
		in       *corev1.ResourceRequirements
		want     *corev1.ResourceRequirements
	}{
		{
			name: "resources are empty",
			defaults: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"resource": resource.MustParse("100m"),
				},
				Limits: corev1.ResourceList{
					"resource": resource.MustParse("100m"),
				},
			},
			in: &corev1.ResourceRequirements{},
			want: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"resource": resource.MustParse("100m"),
				},
				Limits: corev1.ResourceList{
					"resource": resource.MustParse("100m"),
				},
			},
		},
		{
			name: "requests are not empty",
			defaults: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"same":  resource.MustParse("100m"),
					"other": resource.MustParse("200m"),
				},
			},
			in: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"same": resource.MustParse("500m"),
					"new":  resource.MustParse("300m"),
				},
			},
			want: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"same":  resource.MustParse("500m"),
					"new":   resource.MustParse("200m"),
					"other": resource.MustParse("300m"),
				},
			},
		},
		{
			name: "limits are not empty",
			defaults: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"same":  resource.MustParse("100m"),
					"other": resource.MustParse("200m"),
				},
			},
			in: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"same": resource.MustParse("500m"),
					"new":  resource.MustParse("300m"),
				},
			},
			want: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"same":  resource.MustParse("500m"),
					"new":   resource.MustParse("200m"),
					"other": resource.MustParse("300m"),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applyDefaultResources(test.defaults, test.in)
			if diff := cmp.Diff(test.want, test.in, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" { // Maybe this compare fails
				t.Errorf("ApplyDefaultResources (-want, +got) = %v", diff)
			}
		})
	}
}
