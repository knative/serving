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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/metrics"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"

	. "knative.dev/serving/pkg/testing/v1"
)

var (
	servingContainerName         = "serving-container"
	sidecarContainerName         = "sidecar-container-1"
	sidecarContainerName2        = "sidecar-container-2"
	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
	defaultServingContainer      = &corev1.Container{
		Name:  servingContainerName,
		Image: "busybox",
		Ports: buildContainerPorts(v1.DefaultUserPort),
		VolumeMounts: []corev1.VolumeMount{{
			Name:        varLogVolume.Name,
			MountPath:   "/var/log",
			SubPathExpr: "$(K_INTERNAL_POD_NAMESPACE)_$(K_INTERNAL_POD_NAME)_" + servingContainerName,
		}},
		Lifecycle:                userLifecycle,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Stdin:                    false,
		TTY:                      false,
		Env: []corev1.EnvVar{{
			Name:  "PORT",
			Value: "8080",
		}, {
			Name:  "K_REVISION",
			Value: "bar",
		}, {
			Name: "K_CONFIGURATION",
		}, {
			Name: "K_SERVICE",
		}, {
			Name:      "K_INTERNAL_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}},
		}, {
			Name:      "K_INTERNAL_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
		}},
	}

	defaultQueueContainer = &corev1.Container{
		Name:      QueueContainerName,
		Resources: createQueueResources(&deploymentConfig, make(map[string]string), &corev1.Container{}),
		Ports:     append(queueNonServingPorts, queueHTTPPort),
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe-period", "0"},
				},
			},
			PeriodSeconds:  10,
			TimeoutSeconds: 10,
		},
		SecurityContext: queueSecurityContext,
		Env: []corev1.EnvVar{{
			Name:  "SERVING_NAMESPACE",
			Value: "foo", // matches namespace
		}, {
			Name: "SERVING_SERVICE",
		}, {
			Name: "SERVING_CONFIGURATION",
			// No OwnerReference
		}, {
			Name:  "SERVING_REVISION",
			Value: "bar", // matches name
		}, {
			Name:  "QUEUE_SERVING_PORT",
			Value: "8012",
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
			Name: "SERVING_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		}, {
			Name: "SERVING_LOGGING_CONFIG",
			// No logging configuration
		}, {
			Name: "SERVING_LOGGING_LEVEL",
			// No logging level
		}, {
			Name:  "SERVING_REQUEST_LOG_TEMPLATE",
			Value: "",
		}, {
			Name:  "SERVING_ENABLE_REQUEST_LOG",
			Value: "false",
		}, {
			Name:  "SERVING_REQUEST_METRICS_BACKEND",
			Value: "",
		}, {
			Name:  "TRACING_CONFIG_BACKEND",
			Value: "",
		}, {
			Name:  "TRACING_CONFIG_ZIPKIN_ENDPOINT",
			Value: "",
		}, {
			Name:  "TRACING_CONFIG_STACKDRIVER_PROJECT_ID",
			Value: "",
		}, {
			Name:  "TRACING_CONFIG_DEBUG",
			Value: "false",
		}, {
			Name:  "TRACING_CONFIG_SAMPLE_RATE",
			Value: "0",
		}, {
			Name:  "USER_PORT",
			Value: "8080",
		}, {
			Name:  "SYSTEM_NAMESPACE",
			Value: system.Namespace(),
		}, {
			Name:  "METRICS_DOMAIN",
			Value: metrics.Domain(),
		}, {
			Name:  "SERVING_READINESS_PROBE",
			Value: fmt.Sprintf(`{"tcpSocket":{"port":%d,"host":"127.0.0.1"}}`, v1.DefaultUserPort),
		}, {
			Name:  "ENABLE_PROFILING",
			Value: "false",
		}, {
			Name:  "SERVING_ENABLE_PROBE_REQUEST_LOG",
			Value: "false",
		}, {
			Name:  "METRICS_COLLECTOR_ADDRESS",
			Value: "",
		}},
	}

	defaultPodSpec = &corev1.PodSpec{
		Volumes:                       []corev1.Volume{varLogVolume},
		TerminationGracePeriodSeconds: refInt64(45),
	}

	defaultDeployment = &appsv1.Deployment{
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
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               "Revision",
				Name:               "bar",
				UID:                "1234",
				Controller:         ptr.Bool(true),
				BlockOwnerDeletion: ptr.Bool(true),
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					serving.RevisionUID: "1234",
				},
			},
			ProgressDeadlineSeconds: ptr.Int32(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						AppLabelKey:              "bar",
					},
					Annotations: map[string]string{},
				},
				// Spec: filled in by makePodSpec
			},
		},
	}
)

func defaultSidecarContainer(containerName string) *corev1.Container {
	return &corev1.Container{
		Name:  containerName,
		Image: "ubuntu",
		VolumeMounts: []corev1.VolumeMount{{
			Name:        varLogVolume.Name,
			MountPath:   "/var/log",
			SubPathExpr: "$(K_INTERNAL_POD_NAMESPACE)_$(K_INTERNAL_POD_NAME)_" + containerName,
		}},
		Lifecycle:                userLifecycle,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Stdin:                    false,
		TTY:                      false,
		Env: []corev1.EnvVar{{
			Name:  "K_REVISION",
			Value: "bar",
		}, {
			Name: "K_CONFIGURATION",
		}, {
			Name: "K_SERVICE",
		}, {
			Name:      "K_INTERNAL_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}},
		}, {
			Name:      "K_INTERNAL_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
		}},
	}
}

func defaultRevision() *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			UID: "1234",
		},
		Spec: v1.RevisionSpec{
			TimeoutSeconds: ptr.Int64(45),
		},
	}
}

func refInt64(num int64) *int64 {
	return &num
}

type containerOption func(*corev1.Container)
type podSpecOption func(*corev1.PodSpec)
type deploymentOption func(*appsv1.Deployment)

func container(container *corev1.Container, opts ...containerOption) corev1.Container {
	for _, option := range opts {
		option(container)
	}
	return *container
}

func servingContainer(opts ...containerOption) corev1.Container {
	return container(defaultServingContainer.DeepCopy(), opts...)
}

func sidecarContainer(containerName string, opts ...containerOption) corev1.Container {
	return container(defaultSidecarContainer(containerName).DeepCopy(), opts...)
}

func queueContainer(opts ...containerOption) corev1.Container {
	return container(defaultQueueContainer.DeepCopy(), opts...)
}

func withEnvVar(name, value string) containerOption {
	return func(container *corev1.Container) {
		for i, envVar := range container.Env {
			if envVar.Name == name {
				container.Env[i].Value = value
				return
			}
		}

		container.Env = append(container.Env, corev1.EnvVar{
			Name:  name,
			Value: value,
		})
	}
}

func withTCPReadinessProbe(port int) *corev1.Probe {
	return &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(port),
			}}}
}

func withHTTPReadinessProbe(port int) *corev1.Probe {
	return &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(port),
				Path: "/",
			},
		}}
}

func withExecReadinessProbe(command []string) *corev1.Probe {
	return &corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: command,
			},
		}}
}

func withLivenessProbe(handler corev1.Handler) containerOption {
	return func(container *corev1.Container) {
		container.LivenessProbe = &corev1.Probe{Handler: handler}
	}
}

func withPrependedVolumeMounts(volumeMounts ...corev1.VolumeMount) containerOption {
	return func(c *corev1.Container) {
		c.VolumeMounts = append(volumeMounts, c.VolumeMounts...)
	}
}

func podSpec(containers []corev1.Container, opts ...podSpecOption) *corev1.PodSpec {
	podSpec := defaultPodSpec.DeepCopy()
	podSpec.Containers = containers

	for _, option := range opts {
		option(podSpec)
	}

	return podSpec
}

func withAppendedVolumes(volumes ...corev1.Volume) podSpecOption {
	return func(ps *corev1.PodSpec) {
		ps.Volumes = append(ps.Volumes, volumes...)
	}
}

func appsv1deployment(opts ...deploymentOption) *appsv1.Deployment {
	deploy := defaultDeployment.DeepCopy()
	for _, option := range opts {
		option(deploy)
	}
	return deploy
}

func revision(name, ns string, opts ...RevisionOption) *v1.Revision {
	revision := defaultRevision()
	revision.ObjectMeta.Name = name
	revision.ObjectMeta.Namespace = ns
	for _, option := range opts {
		option(revision)
	}
	return revision
}

func withContainerConcurrency(cc int64) RevisionOption {
	return func(revision *v1.Revision) {
		revision.Spec.ContainerConcurrency = &cc
	}
}

func withoutLabels(revision *v1.Revision) {
	revision.Labels = map[string]string{}
}

func withOwnerReference(name string) RevisionOption {
	return func(revision *v1.Revision) {
		revision.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
			APIVersion:         v1.SchemeGroupVersion.String(),
			Kind:               "Configuration",
			Name:               name,
			Controller:         ptr.Bool(true),
			BlockOwnerDeletion: ptr.Bool(true),
		}}
	}
}

// withContainers function helps to provide a podSpec to the revision for both single and multiple containers
func withContainers(containers []corev1.Container) RevisionOption {
	return func(revision *v1.Revision) {
		revision.Spec = v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: containers,
			},
			TimeoutSeconds: ptr.Int64(45),
		}
	}
}

func getContainer() containerOption {
	return func(container *corev1.Container) {
		container.Command = []string{"/bin/bash"}
		container.Args = []string{"-c", "echo Hello world"}
		container.Env = append([]corev1.EnvVar{{
			Name:  "FOO",
			Value: "bar",
		}, {
			Name:  "BAZ",
			Value: "blah",
		}}, container.Env...)
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("666Mi"),
				corev1.ResourceCPU:    resource.MustParse("666m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("888Mi"),
				corev1.ResourceCPU:    resource.MustParse("888m"),
			},
		}
		container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
}

func updateContainer() containerOption {
	return func(container *corev1.Container) {
		container.Command = []string{"/bin/bash"}
		container.Args = []string{"-c", "echo Hello world"}
		container.Env = append([]corev1.EnvVar{{
			Name:  "FOO",
			Value: "bar",
		}, {
			Name:  "BAZ",
			Value: "blah",
		}}, container.Env...)
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("666Mi"),
				corev1.ResourceCPU:    resource.MustParse("666m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("888Mi"),
				corev1.ResourceCPU:    resource.MustParse("888m"),
			},
		}
		container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
}

func TestMakePodSpec(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		oc   metrics.ObservabilityConfig
		want *corev1.PodSpec
	}{{
		name: "user-defined user port, queue proxy have PORT env",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Ports[0].ContainerPort = 8888
						container.Image = "busybox@sha256:deadbeef"
					},
					withEnvVar("PORT", "8888"),
				),
				queueContainer(
					withEnvVar("USER_PORT", "8888"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8888,"host":"127.0.0.1"}}`),
				)}),
	}, {
		name: "volumes passed through",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "asdf",
					MountPath: "/asdf",
				}},
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			func(revision *v1.Revision) {
				revision.Spec.Volumes = []corev1.Volume{{
					Name: "asdf",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "asdf",
						},
					},
				}}
			},
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Ports[0].ContainerPort = 8888
						container.Image = "busybox@sha256:deadbeef"
					},
					withEnvVar("PORT", "8888"),
					withPrependedVolumeMounts(corev1.VolumeMount{
						Name:      "asdf",
						MountPath: "/asdf",
					}),
				),
				queueContainer(
					withEnvVar("USER_PORT", "8888"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8888,"host":"127.0.0.1"}}`),
				),
			}, withAppendedVolumes(corev1.Volume{
				Name: "asdf",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "asdf",
					},
				},
			})),
	}, {
		name: "concurrency=1 no owner",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			withContainerConcurrency(1),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
				),
			}),
	}, {
		name: "metrics collector address",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		oc: metrics.ObservabilityConfig{
			RequestMetricsBackend:   "opencensus",
			MetricsCollectorAddress: "otel:55678",
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("METRICS_COLLECTOR_ADDRESS", "otel:55678"),
					withEnvVar("SERVING_REQUEST_METRICS_BACKEND", "opencensus"),
				),
			}),
	}, {
		name: "concurrency=121 no owner digest resolved",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			withContainerConcurrency(121),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "121"),
				),
			}),
	}, {
		name: "concurrency=1 with owner",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			withContainerConcurrency(42),
			withOwnerReference("parent-config"),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("SERVING_CONFIGURATION", "parent-config"),
					withEnvVar("CONTAINER_CONCURRENCY", "42"),
				),
			}),
	}, {
		name: "with http readiness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withHTTPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP","httpHeaders":[{"name":"K-Kubelet-Probe","value":"queue"}]}}`),
				),
			}),
	}, {
		name: "with tcp readiness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(12345),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}`),
				),
			}),
	}, {
		name: "with shell readiness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withExecReadinessProbe([]string{"echo", "hello"}),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Image = "busybox@sha256:deadbeef"
						container.ReadinessProbe = withExecReadinessProbe([]string{"echo", "hello"})
					}),
				queueContainer(
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}`),
				),
			}),
	}, {
		name: "with HTTP liveness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Image = "busybox@sha256:deadbeef"
					},
					withLivenessProbe(corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(networking.BackendHTTPPort),
							HTTPHeaders: []corev1.HTTPHeader{{
								Name:  network.KubeletProbeHeaderName,
								Value: queue.Name,
							}},
						},
					}),
				),
				queueContainer(),
			}),
	}, {
		name: "with tcp liveness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{},
					}}}},
			),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Image = "busybox@sha256:deadbeef"
					},
					withLivenessProbe(corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(v1.DefaultUserPort),
						},
					}),
				),
				queueContainer(),
			}),
	}, {
		name: "complex pod spec",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			func(revision *v1.Revision) {
				revision.Labels = map[string]string{
					serving.ConfigurationLabelKey: "cfg",
					serving.ServiceLabelKey:       "svc",
				}
				container(revision.Spec.GetContainer(), updateContainer())
			},
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Image = "busybox@sha256:deadbeef"
					},
					getContainer(),
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				queueContainer(
					withEnvVar("SERVING_SERVICE", "svc"),
				)}),
	}, {
		name: "complex pod spec for multiple containers with container data to all containers",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: v1.DefaultUserPort,
				}},
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}, {
				Name:  sidecarContainerName,
				Image: "ubuntu",
			}, {
				Name:  "sidecar-container-2",
				Image: "alpine",
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbffe",
			}, {
				ImageDigest: "alpine@sha256:deadbfff",
			}}),
			func(revision *v1.Revision) {
				revision.Labels = map[string]string{
					serving.ConfigurationLabelKey: "cfg",
					serving.ServiceLabelKey:       "svc",
				}
				for i := range revision.Spec.Containers {
					container(&revision.Spec.Containers[i], updateContainer())
				}
			},
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Image = "busybox@sha256:deadbeef"
					},
					getContainer(),
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				sidecarContainer(sidecarContainerName,
					func(container *corev1.Container) {
						container.Image = "ubuntu@sha256:deadbffe"
					},
					getContainer(),
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				sidecarContainer(sidecarContainerName2,
					getContainer(),
					func(container *corev1.Container) {
						container.Name = "sidecar-container-2"
						container.Image = "alpine@sha256:deadbfff"
					},
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				queueContainer(
					withEnvVar("SERVING_SERVICE", "svc"),
				),
			}),
	}, {
		name: "complex pod spec for multiple containers with container data only to serving containers",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}, {
				Name:  sidecarContainerName,
				Image: "ubuntu",
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbffe",
			}}),
			func(revision *v1.Revision) {
				revision.Labels = map[string]string{
					serving.ConfigurationLabelKey: "cfg",
					serving.ServiceLabelKey:       "svc",
				}
				container(revision.Spec.GetContainer(), updateContainer())
			},
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					func(container *corev1.Container) {
						container.Image = "busybox@sha256:deadbeef"
					},
					getContainer(),
					func(container *corev1.Container) {
						container.Ports[0].ContainerPort = 8888
					},
					withEnvVar("PORT", "8888"),
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				sidecarContainer(sidecarContainerName,
					func(container *corev1.Container) {
						container.Image = "ubuntu@sha256:deadbffe"
					},
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				queueContainer(
					withEnvVar("SERVING_SERVICE", "svc"),
					withEnvVar("USER_PORT", "8888"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8888,"host":"127.0.0.1"}}`),
				),
			}),
	}, {
		name: "propertes allowed by the webhook are passed through",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8080,
				}},
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			func(r *v1.Revision) {
				// TODO - do this generically for all allowed properties
				r.Spec.EnableServiceLinks = ptr.Bool(false)
			}),
		want: podSpec(
			[]corev1.Container{
				servingContainer(
					withEnvVar("PORT", "8080"),
					withEnvVar("K_REVISION", "bar"),
				),
				queueContainer(
					withEnvVar("USER_PORT", "8080"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}`),
				),
			},
			func(p *corev1.PodSpec) {
				p.EnableServiceLinks = ptr.Bool(false)
			},
		),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := revCfg
			cfg.Observability = &test.oc
			got, err := makePodSpec(test.rev, &cfg)
			if err != nil {
				t.Fatal("makePodSpec returned error:", err)
			}
			if diff := cmp.Diff(test.want, got, quantityComparer); diff != "" {
				t.Errorf("makePodSpec (-want, +got) =\n%s", diff)
			}
		})
	}
}

var quantityComparer = cmp.Comparer(func(x, y resource.Quantity) bool {
	return x.Cmp(y) == 0
})

func TestMissingProbeError(t *testing.T) {
	if _, err := MakeDeployment(revision("bar", "foo"), &revCfg); err == nil {
		t.Error("expected error from MakeDeployment")
	}
}

func TestMakeDeployment(t *testing.T) {
	tests := []struct {
		name      string
		rev       *v1.Revision
		want      *appsv1.Deployment
		dc        deployment.Config
		acMutator func(*autoscalerconfig.Config)
	}{{
		name: "with concurrency=1",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
				ReadinessProbe: withTCPReadinessProbe(12345),
			}, {
				Name:  sidecarContainerName,
				Image: "ubuntu",
			}}),
			withoutLabels,
			withContainerConcurrency(1),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbffe",
			}})),
		want: appsv1deployment(),
	}, {
		name: "with owner",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "ubuntu",
				ReadinessProbe: withTCPReadinessProbe(12345),
			}}),
			withoutLabels,
			withOwnerReference("parent-config"),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}})),
		want: appsv1deployment(),
	}, {
		name: "with sidecar annotation override",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "ubuntu",
				ReadinessProbe: withTCPReadinessProbe(12345),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			withoutLabels, func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					sidecarIstioInjectAnnotation: "false",
				}
			}),
		want: appsv1deployment(func(deploy *appsv1.Deployment) {
			deploy.Annotations = kmeta.UnionMaps(deploy.Annotations,
				map[string]string{sidecarIstioInjectAnnotation: "false"})
			deploy.Spec.Template.Annotations = kmeta.UnionMaps(deploy.Spec.Template.Annotations,
				map[string]string{sidecarIstioInjectAnnotation: "false"})
		}),
	}, {
		name: "with ProgressDeadline override",
		dc: deployment.Config{
			ProgressDeadline: 42 * time.Second,
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "ubuntu",
				ReadinessProbe: withTCPReadinessProbe(12345),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}), withoutLabels),
		want: appsv1deployment(func(deploy *appsv1.Deployment) {
			deploy.Spec.ProgressDeadlineSeconds = ptr.Int32(42)
		}),
	}, {
		name: "cluster initial scale",
		acMutator: func(ac *autoscalerconfig.Config) {
			ac.InitialScale = 10
		},
		rev: revision("bar", "foo",
			withoutLabels,
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "ubuntu",
				ReadinessProbe: withTCPReadinessProbe(12345),
			}}),
		),
		want: appsv1deployment(func(deploy *appsv1.Deployment) {
			deploy.Spec.Replicas = ptr.Int32(int32(10))
		}),
	}, {
		name: "cluster initial scale override by revision initial scale",
		acMutator: func(ac *autoscalerconfig.Config) {
			ac.InitialScale = 10
		},
		rev: revision("bar", "foo",
			withoutLabels,
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "ubuntu",
				ReadinessProbe: withTCPReadinessProbe(12345),
			}}),
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{autoscaling.InitialScaleAnnotationKey: "20"}
			},
		),
		want: appsv1deployment(func(deploy *appsv1.Deployment) {
			deploy.Spec.Replicas = ptr.Int32(int32(20))
			deploy.Spec.Template.Annotations = map[string]string{autoscaling.InitialScaleAnnotationKey: "20"}
			deploy.Annotations = map[string]string{autoscaling.InitialScaleAnnotationKey: "20"}
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ac := &autoscalerconfig.Config{
				InitialScale:          1,
				AllowZeroInitialScale: false,
			}
			if test.acMutator != nil {
				test.acMutator(ac)
			}
			// Tested above so that we can rely on it here for brevity.
			cfg := revCfg
			cfg.Autoscaler = ac
			cfg.Deployment = &test.dc
			podSpec, err := makePodSpec(test.rev, &cfg)
			if err != nil {
				t.Fatal("makePodSpec returned error:", err)
			}
			if test.want != nil {
				test.want.Spec.Template.Spec = *podSpec
			}
			// Copy to override
			got, err := MakeDeployment(test.rev, &cfg)
			if err != nil {
				t.Fatal("Got unexpected error:", err)
			}
			if diff := cmp.Diff(test.want, got, quantityComparer); diff != "" {
				t.Errorf("MakeDeployment (-want, +got) =\n%s", diff)
			}
		})
	}
}
