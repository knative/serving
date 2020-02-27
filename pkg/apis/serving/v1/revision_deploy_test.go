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

package v1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/pkg/ptr"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
)

var (
	containerName                = "my-container-name"
	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
	defaultUserContainer         = &corev1.Container{
		Name:                     containerName,
		Image:                    "busybox",
		Ports:                    BuildContainerPorts(DefaultUserPort),
		VolumeMounts:             []corev1.VolumeMount{VarLogVolumeMount},
		Lifecycle:                UserLifecycle,
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
			Name:  "K_CONFIGURATION",
			Value: "cfg",
		}, {
			Name:  "K_SERVICE",
			Value: "svc",
		}},
	}

	defaultPodSpec = &corev1.PodSpec{
		Volumes:                       []corev1.Volume{VarLogVolume},
		TerminationGracePeriodSeconds: refInt64(45),
	}

	defaultRevision = &Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
			Labels: map[string]string{
				serving.ConfigurationLabelKey: "cfg",
				serving.ServiceLabelKey:       "svc",
				serving.RouteLabelKey:         "im-a-route",
			},
		},
		Spec: RevisionSpec{
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
type revisionOption func(*Revision)

func container(container *corev1.Container, opts ...containerOption) *corev1.Container {
	for _, option := range opts {
		option(container)
	}
	return container
}

func userContainer(opts ...containerOption) *corev1.Container {
	return container(defaultUserContainer.DeepCopy(), opts...)
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

func withReadinessProbe(handler corev1.Handler) containerOption {
	return func(container *corev1.Container) {
		container.ReadinessProbe = &corev1.Probe{Handler: handler}
	}
}

func withTCPReadinessProbe() containerOption {
	return withReadinessProbe(corev1.Handler{
		TCPSocket: &corev1.TCPSocketAction{
			Host: "127.0.0.1",
			Port: intstr.FromInt(DefaultUserPort),
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

func revision(opts ...revisionOption) *Revision {
	revision := defaultRevision.DeepCopy()
	for _, option := range opts {
		option(revision)
	}
	return revision
}

func withContainerConcurrency(cc int64) revisionOption {
	return func(revision *Revision) {
		revision.Spec.ContainerConcurrency = &cc
	}
}

func withoutLabels(revision *Revision) {
	revision.ObjectMeta.Labels = map[string]string{}
}

func withOwnerReference(name string) revisionOption {
	return func(revision *Revision) {
		revision.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
			APIVersion:         SchemeGroupVersion.String(),
			Kind:               "Configuration",
			Name:               name,
			Controller:         ptr.Bool(true),
			BlockOwnerDeletion: ptr.Bool(true),
		}}
	}
}

func TestMakeUserContainer(t *testing.T) {
	tests := []struct {
		name string
		rev  *Revision
		want *corev1.Container
	}{{
		name: "user-defined user port, queue proxy have PORT env",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *Revision) {
				revision.Spec.GetContainer().Ports = []corev1.ContainerPort{{
					ContainerPort: 8888,
				}}
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: userContainer(
			func(container *corev1.Container) {
				container.Ports[0].ContainerPort = 8888
			},
			withEnvVar("PORT", "8888"),
		),
	}, {
		name: "volumes passed through",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *Revision) {
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
		want: userContainer(
			func(container *corev1.Container) {
				container.Ports[0].ContainerPort = 8888
			},
			withEnvVar("PORT", "8888"),
			withPrependedVolumeMounts(corev1.VolumeMount{
				Name:      "asdf",
				MountPath: "/asdf",
			}),
		),
	}, {
		name: "concurrency=1 no owner",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *Revision) {
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: userContainer(),
	}, {
		name: "concurrency=1 no owner digest resolved",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *Revision) {
				revision.Status = RevisionStatus{
					ImageDigest: "busybox@sha256:deadbeef",
				}
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: userContainer(func(container *corev1.Container) {
			container.Image = "busybox@sha256:deadbeef"
		}),
	}, {
		name: "concurrency=1 with owner",
		rev: revision(
			withContainerConcurrency(1),
			withOwnerReference("parent-config"),
			func(revision *Revision) {
				container(revision.Spec.GetContainer(),
					withTCPReadinessProbe(),
				)
			},
		),
		want: userContainer(),
	}, {
		name: "with http readiness probe",
		rev: revision(func(revision *Revision) {
			container(revision.Spec.GetContainer(),
				withHTTPReadinessProbe(DefaultUserPort),
			)
		}),
		want: userContainer(),
	}, {
		name: "with tcp readiness probe",
		rev: revision(func(revision *Revision) {
			container(revision.Spec.GetContainer(),
				withReadinessProbe(corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Host: "127.0.0.1",
						Port: intstr.FromInt(12345),
					},
				}),
			)
		}),
		want: userContainer(),
	}, {
		name: "with shell readiness probe",
		rev: revision(func(revision *Revision) {
			container(revision.Spec.GetContainer(),
				withExecReadinessProbe([]string{"echo", "hello"}),
			)
		}),
		want: userContainer(withExecReadinessProbe([]string{"echo", "hello"})),
	}, {
		name: "with http liveness probe",
		rev: revision(func(revision *Revision) {
			container(revision.Spec.GetContainer(),
				withTCPReadinessProbe(),
				withLivenessProbe(corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
					},
				}),
			)
		}),
		want: userContainer(
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
	}, {
		name: "with tcp liveness probe",
		rev: revision(func(revision *Revision) {
			container(revision.Spec.GetContainer(),
				withTCPReadinessProbe(),
				withLivenessProbe(corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{},
				}),
			)
		}),
		want: userContainer(
			withLivenessProbe(corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(DefaultUserPort),
				},
			}),
		),
	}, {
		name: "complex pod spec",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *Revision) {
				revision.ObjectMeta.Labels = map[string]string{}
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
		want: userContainer(
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
			withEnvVar("K_CONFIGURATION", ""),
			withEnvVar("K_SERVICE", ""),
		),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quantityComparer := cmp.Comparer(func(x, y resource.Quantity) bool {
				return x.Cmp(y) == 0
			})

			got := MakeUserContainer(test.rev)
			if diff := cmp.Diff(test.want, got, quantityComparer); diff != "" {
				t.Errorf("MakeUserContainer (-want, +got) = %v", diff)
			}
		})
	}
}
