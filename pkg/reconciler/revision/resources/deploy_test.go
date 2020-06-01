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

	"knative.dev/pkg/metrics"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/network"
)

var (
	containerName                = "my-container-name"
	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
	defaultUserContainer         = &corev1.Container{
		Name:  containerName,
		Image: "busybox",
		Ports: buildContainerPorts(v1.DefaultUserPort),
		VolumeMounts: []corev1.VolumeMount{{
			Name:        varLogVolume.Name,
			MountPath:   "/var/log",
			SubPathExpr: "$(K_INTERNAL_POD_NAMESPACE)_$(K_INTERNAL_POD_NAME)_my-container-name",
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
		Resources: createQueueResources(make(map[string]string), &corev1.Container{}),
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
			Name:  "DOWNWARD_API_LABELS_PATH",
			Value: fmt.Sprintf("%s/%s", podInfoVolumePath, metadataLabelsPath),
		}, {
			Name:  "SERVING_READINESS_PROBE",
			Value: fmt.Sprintf(`{"tcpSocket":{"port":%d,"host":"127.0.0.1"}}`, v1.DefaultUserPort),
		}, {
			Name:  "ENABLE_PROFILING",
			Value: "false",
		}, {
			Name:  "SERVING_ENABLE_PROBE_REQUEST_LOG",
			Value: "false",
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
			ProgressDeadlineSeconds: ptr.Int32(int32(deployment.ProgressDeadlineDefault.Seconds())),
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

	defaultRevision = &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			UID: "1234",
		},
		Spec: v1.RevisionSpec{
			TimeoutSeconds: ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  containerName,
					Image: "busybox",
				}},
			},
		},
	}
)

func refInt64(num int64) *int64 {
	return &num
}

type containerOption func(*corev1.Container)
type podSpecOption func(*corev1.PodSpec)
type deploymentOption func(*appsv1.Deployment)
type revisionOption func(*v1.Revision)

func container(container *corev1.Container, opts ...containerOption) corev1.Container {
	for _, option := range opts {
		option(container)
	}
	return *container
}

func userContainer(opts ...containerOption) corev1.Container {
	return container(defaultUserContainer.DeepCopy(), opts...)
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

func withPodLabelsVolumeMount() containerOption {
	return func(container *corev1.Container) {
		container.VolumeMounts = append(container.VolumeMounts, labelVolumeMount)
	}
}

func withReadinessProbe(handler corev1.Handler) containerOption {
	return func(container *corev1.Container) {
		container.ReadinessProbe = &corev1.Probe{Handler: handler}
	}
}

func withTCPReadinessProbe() containerOption {
	return withReadinessProbe(corev1.Handler{
		TCPSocket: &corev1.TCPSocketAction{
			Host: "127.0.0.1",
			Port: intstr.FromInt(v1.DefaultUserPort),
		},
	})
}

func withHTTPReadinessProbe(port int) containerOption {
	return withReadinessProbe(corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port: intstr.FromInt(port),
			Path: "/",
		},
	})
}

func withExecReadinessProbe(command []string) containerOption {
	return withReadinessProbe(corev1.Handler{
		Exec: &corev1.ExecAction{
			Command: command,
		},
	})
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

func makeDeployment(opts ...deploymentOption) *appsv1.Deployment {
	deploy := defaultDeployment.DeepCopy()
	for _, option := range opts {
		option(deploy)
	}
	return deploy
}

func revision(name, ns string, opts ...revisionOption) *v1.Revision {
	revision := defaultRevision.DeepCopy()
	revision.ObjectMeta.Name = name
	revision.ObjectMeta.Namespace = ns
	for _, option := range opts {
		option(revision)
	}
	return revision
}

func withContainerConcurrency(cc int64) revisionOption {
	return func(revision *v1.Revision) {
		revision.Spec.ContainerConcurrency = &cc
	}
}

func withoutLabels(revision *v1.Revision) {
	revision.ObjectMeta.Labels = map[string]string{}
}

func withOwnerReference(name string) revisionOption {
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

func TestMakePodSpec(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		oc   metrics.ObservabilityConfig
		ac   autoscalerconfig.Config
		want *corev1.PodSpec
	}{{
		name: "user-defined user port, queue proxy have PORT env",
		rev: revision("bar", "foo",
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.GetContainer().Ports = []corev1.ContainerPort{{
					ContainerPort: 8888,
				}}
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: podSpec(
			[]corev1.Container{
				userContainer(
					func(container *corev1.Container) {
						container.Ports[0].ContainerPort = 8888
					},
					withEnvVar("PORT", "8888"),
				),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
					withEnvVar("USER_PORT", "8888"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8888,"host":"127.0.0.1"}}`),
				),
			}),
	}, {
		name: "volumes passed through",
		rev: revision("bar", "foo",
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.GetContainer().Ports = []corev1.ContainerPort{{
					ContainerPort: 8888,
				}}
				revision.Spec.GetContainer().VolumeMounts = []corev1.VolumeMount{{
					Name:      "asdf",
					MountPath: "/asdf",
				}}
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
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
				userContainer(
					func(container *corev1.Container) {
						container.Ports[0].ContainerPort = 8888
					},
					withEnvVar("PORT", "8888"),
					withPrependedVolumeMounts(corev1.VolumeMount{
						Name:      "asdf",
						MountPath: "/asdf",
					}),
				),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
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
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: podSpec(
			[]corev1.Container{
				userContainer(),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
				),
			}),
	}, {
		name: "concurrency=1 no owner digest resolved",
		rev: revision("bar", "foo",
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Status = v1.RevisionStatus{
					DeprecatedImageDigest: "busybox@sha256:deadbeef",
				}
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: podSpec(
			[]corev1.Container{
				userContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
				),
			}),
	}, {
		name: "concurrency=1 with owner",
		rev: revision("bar", "foo",
			withContainerConcurrency(1),
			withOwnerReference("parent-config"),
			func(revision *v1.Revision) {
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: podSpec(
			[]corev1.Container{
				userContainer(),
				queueContainer(
					withEnvVar("SERVING_CONFIGURATION", "parent-config"),
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
				),
			}),
	}, {
		name: "with http readiness probe",
		rev: revision("bar", "foo", func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withHTTPReadinessProbe(v1.DefaultUserPort),
			)
		}),
		want: podSpec(
			[]corev1.Container{
				userContainer(),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "0"),
					withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP","httpHeaders":[{"name":"K-Kubelet-Probe","value":"queue"}]}}`),
				),
			}),
	}, {
		name: "with tcp readiness probe",
		rev: revision("bar", "foo", func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withReadinessProbe(corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Host: "127.0.0.1",
						Port: intstr.FromInt(12345),
					},
				}),
			)
		}),
		want: podSpec(
			[]corev1.Container{
				userContainer(),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "0"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}`),
				),
			}),
	}, {
		name: "with shell readiness probe",
		rev: revision("bar", "foo", func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withExecReadinessProbe([]string{"echo", "hello"}),
			)
		}),
		want: podSpec(
			[]corev1.Container{
				userContainer(
					withExecReadinessProbe([]string{"echo", "hello"})),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "0"),
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}`),
				),
			}),
	}, {
		name: "with http liveness probe",
		rev: revision("bar", "foo", func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withTCPReadinessProbe(),
				withLivenessProbe(corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
					},
				}),
			)
		}),
		want: podSpec(
			[]corev1.Container{
				userContainer(
					withLivenessProbe(corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(networking.BackendHTTPPort),
							HTTPHeaders: []corev1.HTTPHeader{{
								Name:  network.KubeletProbeHeaderName,
								Value: "queue",
							}},
						},
					}),
				),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "0"),
				),
			}),
	}, {
		name: "with tcp liveness probe",
		rev: revision("bar", "foo", func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withTCPReadinessProbe(),
				withLivenessProbe(corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{},
				}),
			)
		}),
		want: podSpec(
			[]corev1.Container{
				userContainer(
					withLivenessProbe(corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(v1.DefaultUserPort),
						},
					}),
				),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "0"),
				),
			}),
	}, {
		name: "with graceful scaledown enabled",
		rev: revision("bar", "foo", func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withTCPReadinessProbe(),
			)
		}),
		ac: autoscalerconfig.Config{
			EnableGracefulScaledown: true,
		},
		want: podSpec(
			[]corev1.Container{
				userContainer(),
				queueContainer(
					withEnvVar("DOWNWARD_API_LABELS_PATH", fmt.Sprintf("%s/%s", podInfoVolumePath, metadataLabelsPath)),
					withPodLabelsVolumeMount(),
				),
			},
			func(podSpec *corev1.PodSpec) {
				podSpec.Volumes = append(podSpec.Volumes, labelVolume)
			},
		),
	}, {
		name: "complex pod spec",
		rev: revision("bar", "foo",
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.ObjectMeta.Labels = map[string]string{
					serving.ConfigurationLabelKey: "cfg",
					serving.ServiceLabelKey:       "svc",
				}
				revision.Spec.GetContainer().Command = []string{"/bin/bash"}
				revision.Spec.GetContainer().Args = []string{"-c", "echo Hello world"}
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
					withEnvVar("FOO", "bar"),
					withEnvVar("BAZ", "blah"),
				)
				revision.Spec.GetContainer().Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("666Mi"),
						corev1.ResourceCPU:    resource.MustParse("666m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("888Mi"),
						corev1.ResourceCPU:    resource.MustParse("888m"),
					},
				}
				revision.Spec.GetContainer().TerminationMessagePolicy = corev1.TerminationMessageReadFile
			},
		),
		want: podSpec(
			[]corev1.Container{
				userContainer(
					func(container *corev1.Container) {
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
					},
					withEnvVar("K_CONFIGURATION", "cfg"),
					withEnvVar("K_SERVICE", "svc"),
				),
				queueContainer(
					withEnvVar("CONTAINER_CONCURRENCY", "1"),
					withEnvVar("SERVING_SERVICE", "svc"),
				),
			}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quantityComparer := cmp.Comparer(func(x, y resource.Quantity) bool {
				return x.Cmp(y) == 0
			})
			got, err := makePodSpec(test.rev, &logConfig, &traceConfig, &test.oc, &test.ac, &deploymentConfig)
			if err != nil {
				t.Fatal("makePodSpec returned error:", err)
			}
			if diff := cmp.Diff(test.want, got, quantityComparer); diff != "" {
				t.Errorf("makePodSpec (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMissingProbeError(t *testing.T) {
	if _, err := MakeDeployment(revision("bar", "foo"), &logConfig, &traceConfig,
		&network.Config{}, &obsConfig, &asConfig, &deploymentConfig); err == nil {
		t.Error("expected error from MakeDeployment")
	}
}

func TestMakeDeployment(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		want *appsv1.Deployment
	}{{
		name: "with concurrency=1",
		rev: revision("bar", "foo",
			withoutLabels,
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				container(revision.Spec.GetContainer(),
					withReadinessProbe(corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Host: "127.0.0.1",
							Port: intstr.FromInt(12345),
						},
					}),
				)
			},
		),
		want: makeDeployment(),
	}, {
		name: "with owner",
		rev: revision("bar", "foo",
			withoutLabels,
			withOwnerReference("parent-config"),
			func(revision *v1.Revision) {
				container(revision.Spec.GetContainer(),
					withReadinessProbe(corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Host: "127.0.0.1",
							Port: intstr.FromInt(12345),
						},
					}),
				)
			},
		),
		want: makeDeployment(),
	}, {
		name: "with sidecar annotation override",
		rev: revision("bar", "foo", withoutLabels, func(revision *v1.Revision) {
			revision.ObjectMeta.Annotations = map[string]string{
				sidecarIstioInjectAnnotation: "false",
			}
			container(revision.Spec.GetContainer(),
				withReadinessProbe(corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Host: "127.0.0.1",
						Port: intstr.FromInt(12345),
					},
				}),
			)
		}),
		want: makeDeployment(func(deploy *appsv1.Deployment) {
			deploy.ObjectMeta.Annotations[sidecarIstioInjectAnnotation] = "false"
			deploy.Spec.Template.ObjectMeta.Annotations[sidecarIstioInjectAnnotation] = "false"
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Tested above so that we can rely on it here for brevity.
			podSpec, err := makePodSpec(test.rev, &logConfig, &traceConfig,
				&obsConfig, &asConfig, &deploymentConfig)
			if err != nil {
				t.Fatal("makePodSpec returned error:", err)
			}
			test.want.Spec.Template.Spec = *podSpec
			got, err := MakeDeployment(test.rev, &logConfig, &traceConfig,
				&network.Config{}, &obsConfig, &asConfig, &deploymentConfig)
			if err != nil {
				t.Fatal("got unexpected error:", err)
			}
			if diff := cmp.Diff(test.want, got, cmp.AllowUnexported(resource.Quantity{})); diff != "" {
				t.Error("MakeDeployment (-want, +got) =", diff)
			}
		})
	}
}

func TestProgressDeadlineOverride(t *testing.T) {
	rev := revision("bar", "foo",
		withoutLabels,
		func(revision *v1.Revision) {
			container(revision.Spec.GetContainer(),
				withReadinessProbe(corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Host: "127.0.0.1",
						Port: intstr.FromInt(12345),
					},
				}),
			)
		},
	)
	want := makeDeployment(func(d *appsv1.Deployment) {
		d.Spec.ProgressDeadlineSeconds = ptr.Int32(42)
	})

	dc := &deployment.Config{
		ProgressDeadline: 42 * time.Second,
	}
	podSpec, err := makePodSpec(rev, &logConfig, &traceConfig, &obsConfig, &asConfig, dc)
	if err != nil {
		t.Fatal("makePodSpec returned error:", err)
	}
	want.Spec.Template.Spec = *podSpec
	got, err := MakeDeployment(rev, &logConfig, &traceConfig,
		&network.Config{}, &obsConfig, &asConfig, dc)
	if err != nil {
		t.Fatal("MakeDeployment returned error:", err)
	}
	if !cmp.Equal(want, got, cmp.AllowUnexported(resource.Quantity{})) {
		t.Error("MakeDeployment (-want, +got) =", cmp.Diff(want, got, cmp.AllowUnexported(resource.Quantity{})))
	}
}
