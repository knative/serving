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
	"knative.dev/pkg/kmap"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/autoscaling"
	apicfg "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/observability"
	"knative.dev/serving/pkg/queue"

	. "knative.dev/serving/pkg/testing/v1"
)

var (
	servingContainerName    = "serving-container"
	sidecarContainerName    = "sidecar-container-1"
	sidecarContainerName2   = "sidecar-container-2"
	sidecarIstioInjectLabel = "sidecar.istio.io/inject"
	defaultServingContainer = &corev1.Container{
		Name:                     servingContainerName,
		Image:                    "busybox",
		Ports:                    buildContainerPorts(v1.DefaultUserPort),
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
		}},
	}

	defaultQueueContainer = &corev1.Container{
		Name:      QueueContainerName,
		Resources: createQueueResources(&deploymentConfig, make(map[string]string), &corev1.Container{}, false),
		Ports:     append(queueNonServingPorts, queueHTTPPort, queueHTTPSPort),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt32(queueHTTPPort.ContainerPort),
					HTTPHeaders: []corev1.HTTPHeader{{
						Name:  netheader.ProbeKey,
						Value: queue.Name,
					}},
				},
			},
			PeriodSeconds: 0,
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
			Name:  "QUEUE_SERVING_TLS_PORT",
			Value: "8112",
		}, {
			Name:  "CONTAINER_CONCURRENCY",
			Value: "0",
		}, {
			Name:  "REVISION_TIMEOUT_SECONDS",
			Value: "45",
		}, {
			Name:  "REVISION_RESPONSE_START_TIMEOUT_SECONDS",
			Value: "0",
		}, {
			Name:  "REVISION_IDLE_TIMEOUT_SECONDS",
			Value: "0",
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
			Name:  "USER_PORT",
			Value: "8080",
		}, {
			Name:  "SYSTEM_NAMESPACE",
			Value: system.Namespace(),
		}, {
			Name:  "SERVING_READINESS_PROBE",
			Value: fmt.Sprintf(`{"tcpSocket":{"port":%d,"host":"127.0.0.1"}}`, v1.DefaultUserPort),
		}, {
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "status.hostIP"},
			},
		}, {
			Name:  "ENABLE_HTTP2_AUTO_DETECTION",
			Value: "false",
		}, {
			Name:  "ENABLE_HTTP_FULL_DUPLEX",
			Value: "false",
		}, {
			Name:  "ROOT_CA",
			Value: "",
		}, {
			Name:  "ENABLE_MULTI_CONTAINER_PROBES",
			Value: "false",
		}, {
			Name:  "OBSERVABILITY_CONFIG",
			Value: `{"tracing":{},"metrics":{},"runtime":{},"requestMetrics":{}}`,
		}},
	}

	defaultPodSpec = &corev1.PodSpec{
		TerminationGracePeriodSeconds: ptr.Int64(45),
		EnableServiceLinks:            ptr.Bool(false),
	}

	defaultPodAntiAffinityRules = &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				TopologyKey: "kubernetes.io/hostname",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"serving.knative.dev/revision": "bar",
					},
				},
			},
		}},
	}

	userDefinedPodAntiAffinityRules = &corev1.PodAntiAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
			TopologyKey: "kubernetes.io/hostname",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"serving.knative.dev/revision": "bar",
				},
			},
		}},
	}

	maxUnavailable    = intstr.FromInt32(0)
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
			RevisionHistoryLimit:    ptr.Int32(0),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &maxUnavailable,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						serving.RevisionLabelKey: "bar",
						serving.RevisionUID:      "1234",
						AppLabelKey:              "bar",
					},
					Annotations: map[string]string{
						DefaultContainerAnnotationName: servingContainerName,
					},
				},
				// Spec: filled in by makePodSpec
			},
		},
	}
)

func defaultSidecarContainer(containerName string) *corev1.Container {
	return &corev1.Container{
		Name:                     containerName,
		Image:                    "ubuntu",
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

type (
	containerOption  func(*corev1.Container)
	podSpecOption    func(*corev1.PodSpec)
	deploymentOption func(*appsv1.Deployment)
)

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

func withPodInfoVolumeMount() containerOption {
	return func(container *corev1.Container) {
		container.VolumeMounts = append(container.VolumeMounts, varPodInfoVolumeMount)
	}
}

func withTCPReadinessProbe(port int) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(port),
			},
		},
	}
}

func withHTTPReadinessProbe(port int) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(port),
				Path: "/",
			},
		},
	}
}

func withGRPCReadinessProbe(port int) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			GRPC: &corev1.GRPCAction{
				Port: int32(port),
			},
		},
	}
}

func withExecReadinessProbe(command []string) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: command,
			},
		},
	}
}

func withLivenessProbe(handler corev1.ProbeHandler) containerOption {
	return func(container *corev1.Container) {
		container.LivenessProbe = &corev1.Probe{ProbeHandler: handler}
	}
}

func withStartupProbe(handler corev1.ProbeHandler) containerOption {
	return func(container *corev1.Container) {
		container.StartupProbe = &corev1.Probe{ProbeHandler: handler}
	}
}

func withPrependedVolumeMounts(volumeMounts ...corev1.VolumeMount) containerOption {
	return func(c *corev1.Container) {
		c.VolumeMounts = append(volumeMounts, c.VolumeMounts...)
	}
}

func withRuntimeClass(name string) podSpecOption {
	return func(ps *corev1.PodSpec) {
		ps.RuntimeClassName = ptr.String(name)
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

type appendTokenVolume struct {
	filename string
	audience string
	expires  int64
}

func withAppendedTokenVolumes(appended []appendTokenVolume) podSpecOption {
	return func(ps *corev1.PodSpec) {
		tokenVolume := varTokenVolume.DeepCopy()
		for _, a := range appended {
			token := &corev1.VolumeProjection{
				ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
					ExpirationSeconds: ptr.Int64(a.expires),
					Path:              a.filename,
					Audience:          a.audience,
				},
			}
			tokenVolume.VolumeSource.Projected.Sources = append(tokenVolume.VolumeSource.Projected.Sources, *token)
		}
		ps.Volumes = append(ps.Volumes, *tokenVolume)
	}
}

func withAppendedVolumes(volumes ...corev1.Volume) podSpecOption {
	return func(ps *corev1.PodSpec) {
		ps.Volumes = append(ps.Volumes, volumes...)
	}
}

func withPrependedVolumes(volumes ...corev1.Volume) podSpecOption {
	return func(ps *corev1.PodSpec) {
		ps.Volumes = append(volumes, ps.Volumes...)
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

// WithRevisionAnnotations adds the supplied annotations to the revision
func WithRevisionAnnotations(annotations map[string]string) RevisionOption {
	return func(revision *v1.Revision) {
		revision.Annotations = kmeta.UnionMaps(revision.Annotations, annotations)
	}
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
		name     string
		rev      *v1.Revision
		oc       observability.Config
		defaults *apicfg.Defaults
		dc       deployment.Config
		fc       apicfg.Features
		want     *corev1.PodSpec
	}{{
		name: "user-defined user port, queue proxy have PORT env",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:  servingContainerName,
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
				ReadinessProbe: withTCPReadinessProbe(8888),
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
				),
			}),
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
				ReadinessProbe: withTCPReadinessProbe(8888),
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
			}, withPrependedVolumes(corev1.Volume{
				Name: "asdf",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "asdf",
					},
				},
			})),
	}, {
		name: "explicit true service links",
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
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			}, func(p *corev1.PodSpec) {
				p.EnableServiceLinks = ptr.Bool(true)
			}),
		defaults: func() *apicfg.Defaults {
			d, _ := apicfg.NewDefaultsConfigFromMap(map[string]string{
				"enable-service-links": "true",
			})
			return d
		}(),
	}, {
		name: "explicit default service links",
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
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			}, func(p *corev1.PodSpec) {
				p.EnableServiceLinks = nil
			}),
		defaults: func() *apicfg.Defaults {
			d, _ := apicfg.NewDefaultsConfigFromMap(map[string]string{
				"enable-service-links": "default",
			})
			return d
		}(),
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
		oc: observability.Config{
			RequestMetrics: observability.MetricsConfig{
				Endpoint: "otel:55678",
				Protocol: "http/protobuf",
			},
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("OBSERVABILITY_CONFIG", `{"tracing":{},"metrics":{},"runtime":{},"requestMetrics":{"protocol":"http/protobuf","endpoint":"otel:55678"}}`),
				),
			}),
	}, {
		name: "podInfoFeature Enabled",
		fc: apicfg.Features{
			QueueProxyMountPodInfo: apicfg.Enabled,
		},
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
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withPodInfoVolumeMount(),
				),
			},
			withAppendedVolumes(varPodInfoVolume),
		),
	}, {
		name: "podInfoFeature Disabled and enabled using annotation ",
		fc: apicfg.Features{
			QueueProxyMountPodInfo: apicfg.Disabled,
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			WithRevisionAnnotations(map[string]string{apicfg.QueueProxyPodInfoFeatureKey: string(apicfg.Enabled)}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			},
		),
	}, {
		name: "podInfoFeature Allowed and enabled using annotation",
		fc: apicfg.Features{
			QueueProxyMountPodInfo: apicfg.Allowed,
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			WithRevisionAnnotations(map[string]string{apicfg.QueueProxyPodInfoFeatureKey: string(apicfg.Enabled)}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withPodInfoVolumeMount(),
				),
			},
			withAppendedVolumes(varPodInfoVolume),
		),
	}, {
		name: "podInfoFeature Allowed and Disabled using annotation",
		fc: apicfg.Features{
			QueueProxyMountPodInfo: apicfg.Allowed,
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			WithRevisionAnnotations(map[string]string{apicfg.QueueProxyPodInfoFeatureKey: string(apicfg.Disabled)}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			},
		),
	}, {
		name: "podInfoFeature Allowed without annotation",
		fc: apicfg.Features{
			QueueProxyMountPodInfo: apicfg.Allowed,
		},
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
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			},
		),
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
					withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP"}}`),
				),
			}),
	}, {
		name: "with grpc readiness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withGRPCReadinessProbe(v1.DefaultUserPort),
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
					withEnvVar("SERVING_READINESS_PROBE", `{"grpc":{"port":8080,"service":null}}`),
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
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":12345,"host":"127.0.0.1"}}`),
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
					ProbeHandler: corev1.ProbeHandler{
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
					withLivenessProbe(corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt32(v1.DefaultUserPort),
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
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{},
					},
				},
			}},
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
					withLivenessProbe(corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt32(v1.DefaultUserPort),
						},
					}),
				),
				queueContainer(),
			}),
	}, {
		name: "with HTTP startup probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				StartupProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
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
					withStartupProbe(corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					}),
				),
				queueContainer(),
			}),
	}, {
		name: "with TCP startup probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				StartupProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{},
					},
				},
			}},
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
					withStartupProbe(corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{},
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
				),
			}),
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
				ReadinessProbe: withTCPReadinessProbe(8888),
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
		name: "properties allowed by the webhook are passed through",
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
	}, {
		name: "var-log collection enabled",
		oc: observability.Config{
			EnableVarLogCollection: true,
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				Ports:          buildContainerPorts(v1.DefaultUserPort),
			}, {
				Name:  sidecarContainerName,
				Image: "ubuntu",
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
					container.VolumeMounts = []corev1.VolumeMount{{
						Name:        varLogVolume.Name,
						MountPath:   "/var/log",
						SubPathExpr: "$(K_INTERNAL_POD_NAMESPACE)_$(K_INTERNAL_POD_NAME)_" + servingContainerName,
					}}
					container.Env = append(container.Env,
						corev1.EnvVar{
							Name:      "K_INTERNAL_POD_NAME",
							ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}},
						},
						corev1.EnvVar{
							Name:      "K_INTERNAL_POD_NAMESPACE",
							ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
						})
				}),
				sidecarContainer(sidecarContainerName, func(c *corev1.Container) {
					c.Image = "ubuntu@sha256:deadbeef"
					c.VolumeMounts = []corev1.VolumeMount{{
						Name:        varLogVolume.Name,
						MountPath:   "/var/log",
						SubPathExpr: "$(K_INTERNAL_POD_NAMESPACE)_$(K_INTERNAL_POD_NAME)_" + sidecarContainerName,
					}}
					c.Env = append(c.Env,
						corev1.EnvVar{
							Name:      "K_INTERNAL_POD_NAME",
							ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}},
						},
						corev1.EnvVar{
							Name:      "K_INTERNAL_POD_NAMESPACE",
							ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
						})
				}),
				queueContainer(
					withEnvVar("SERVING_READINESS_PROBE", `{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}`),
					withEnvVar("OBSERVABILITY_CONFIG", `{"tracing":{},"metrics":{},"runtime":{},"requestMetrics":{},"EnableVarLogCollection":true}`),
				),
			},
			withAppendedVolumes(varLogVolume),
		),
	}, {
		name: "with no readiness probe",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Image: "busybox",
				Name:  servingContainerName,
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.ReadinessProbe = nil
				}),
				queueContainer(
					withEnvVar("SERVING_READINESS_PROBE", ""),
					func(container *corev1.Container) {
						container.ReadinessProbe = nil
					}),
			}),
	}, {
		name: "qpoption tokens",
		dc: deployment.Config{
			QueueSidecarTokenAudiences: sets.New("boo-srv"),
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				Ports:          buildContainerPorts(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(func(container *corev1.Container) {
					container.VolumeMounts = []corev1.VolumeMount{{
						Name:      varTokenVolume.Name,
						MountPath: "/var/run/secrets/tokens",
					}}
				}),
			},
			withAppendedTokenVolumes([]appendTokenVolume{{filename: "boo-srv", audience: "boo-srv", expires: 3600}}),
		),
	}, {
		name: "qpoption rootca",
		dc: deployment.Config{
			QueueSidecarRootCA: "myCertificate",
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
				Ports:          buildContainerPorts(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbeef",
			}}),
		),
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(
					withEnvVar("ROOT_CA", `myCertificate`),
				),
			},
		),
	}, {
		name: "with multiple containers with readiness probes",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				Ports:          buildContainerPorts(v1.DefaultUserPort),
				ReadinessProbe: withHTTPReadinessProbe(v1.DefaultUserPort),
			}, {
				Name:           sidecarContainerName,
				Image:          "Ubuntu",
				ReadinessProbe: withHTTPReadinessProbe(8090),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbffe",
			}}),
		),
		fc: apicfg.Features{
			MultiContainerProbing: apicfg.Enabled,
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				sidecarContainer(sidecarContainerName,
					func(container *corev1.Container) {
						container.Image = "ubuntu@sha256:deadbffe"
					},
				),
				queueContainer(
					withEnvVar("ENABLE_MULTI_CONTAINER_PROBES", "true"),
					withEnvVar("SERVING_READINESS_PROBE", `[{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP"}},{"httpGet":{"path":"/","port":8090,"host":"127.0.0.1","scheme":"HTTP"}}]`),
				),
			}),
	}, {
		name: "with multiple containers with exec probes",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				Ports:          buildContainerPorts(v1.DefaultUserPort),
				ReadinessProbe: withExecReadinessProbe([]string{"bin/sh", "serving.sh"}),
			}, {
				Name:           sidecarContainerName,
				Image:          "Ubuntu",
				ReadinessProbe: withExecReadinessProbe([]string{"bin/sh", "sidecar.sh"}),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}, {
				ImageDigest: "ubuntu@sha256:deadbffe",
			}}),
		),
		fc: apicfg.Features{
			MultiContainerProbing: apicfg.Enabled,
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
					container.ReadinessProbe = withExecReadinessProbe([]string{"bin/sh", "serving.sh"})
				}),
				sidecarContainer(sidecarContainerName,
					func(container *corev1.Container) {
						container.Image = "ubuntu@sha256:deadbffe"
						container.ReadinessProbe = withExecReadinessProbe([]string{"bin/sh", "sidecar.sh"})
					},
				),
				queueContainer(
					withEnvVar("ENABLE_MULTI_CONTAINER_PROBES", "true"),
					withEnvVar("SERVING_READINESS_PROBE", `[{"tcpSocket":{"port":8080,"host":"127.0.0.1"}}]`),
				),
			}),
	}, {
		name: "with default affinity type set",
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
		fc: apicfg.Features{
			PodSpecAffinity: apicfg.Disabled,
		},
		dc: deployment.Config{
			DefaultAffinityType: deployment.PreferSpreadRevisionOverNodes,
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			},
			func(p *corev1.PodSpec) {
				p.Affinity = &corev1.Affinity{
					PodAntiAffinity: defaultPodAntiAffinityRules,
				}
			},
		),
	}, {
		name: "with default affinity type deactivated",
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
		fc: apicfg.Features{
			PodSpecAffinity: apicfg.Disabled,
		},
		dc: deployment.Config{
			DefaultAffinityType: deployment.None,
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			},
		),
	}, {
		name: "with affinity rules set by both the user and the operator",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
			}}),
			WithContainerStatuses([]v1.ContainerStatus{{
				ImageDigest: "busybox@sha256:deadbeef",
			}}),
			func(r *v1.Revision) {
				r.Spec.Affinity = &corev1.Affinity{
					PodAntiAffinity: userDefinedPodAntiAffinityRules,
				}
			}),
		fc: apicfg.Features{
			PodSpecAffinity: apicfg.Enabled,
		},
		dc: deployment.Config{
			DefaultAffinityType: deployment.PreferSpreadRevisionOverNodes,
		},
		want: podSpec(
			[]corev1.Container{
				servingContainer(func(container *corev1.Container) {
					container.Image = "busybox@sha256:deadbeef"
				}),
				queueContainer(),
			},
			func(p *corev1.PodSpec) {
				p.Affinity = &corev1.Affinity{
					PodAntiAffinity: userDefinedPodAntiAffinityRules,
				}
			},
		),
	}, {
		name: "with runtime-class-name set",
		dc: deployment.Config{
			RuntimeClassNames: map[string]deployment.RuntimeClassNameLabelSelector{
				"gvisor": {},
			},
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				Ports:          buildContainerPorts(v1.DefaultUserPort),
				ReadinessProbe: withHTTPReadinessProbe(v1.DefaultUserPort),
			}}),
		),
		want: podSpec([]corev1.Container{
			servingContainer(func(container *corev1.Container) {
				container.Image = "busybox"
			}),
			queueContainer(
				withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP"}}`),
			),
		}, withRuntimeClass("gvisor")),
	}, {
		name: "with runtime-class-name set requiring selector and no label set in revision",
		dc: deployment.Config{
			RuntimeClassNames: map[string]deployment.RuntimeClassNameLabelSelector{
				"gvisor": {
					Selector: map[string]string{
						"this-one": "specifically",
					},
				},
			},
		},
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				Ports:          buildContainerPorts(v1.DefaultUserPort),
				ReadinessProbe: withHTTPReadinessProbe(v1.DefaultUserPort),
			}}),
		),
		want: podSpec([]corev1.Container{
			servingContainer(func(container *corev1.Container) {
				container.Image = "busybox"
			}),
			queueContainer(
				withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP"}}`),
			),
		}),
	}, {
		name: "with runtime-class-name set requiring selector and label set in revision",
		dc: deployment.Config{
			RuntimeClassNames: map[string]deployment.RuntimeClassNameLabelSelector{
				"gvisor": {
					Selector: map[string]string{
						"this-one": "specifically",
					},
				},
			},
		},
		rev: revision("bar", "foo",
			WithRevisionLabel("this-one", "specifically"),
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				Ports:          buildContainerPorts(v1.DefaultUserPort),
				ReadinessProbe: withHTTPReadinessProbe(v1.DefaultUserPort),
			}}),
		),
		want: podSpec([]corev1.Container{
			servingContainer(func(container *corev1.Container) {
				container.Image = "busybox"
			}),
			queueContainer(
				withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP"}}`),
			),
		}, withRuntimeClass("gvisor")),
	}, {
		name: "with multiple runtime-class-name set and label selector for one",
		dc: deployment.Config{
			RuntimeClassNames: map[string]deployment.RuntimeClassNameLabelSelector{
				"gvisor": {},
				"kata": {
					Selector: map[string]string{
						"specific": "this-one",
					},
				},
			},
		},
		rev: revision("bar", "foo",
			WithRevisionLabel("specific", "this-one"),
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				Image:          "busybox",
				Ports:          buildContainerPorts(v1.DefaultUserPort),
				ReadinessProbe: withHTTPReadinessProbe(v1.DefaultUserPort),
			}}),
		),
		want: podSpec([]corev1.Container{
			servingContainer(func(container *corev1.Container) {
				container.Image = "busybox"
			}),
			queueContainer(
				withEnvVar("SERVING_READINESS_PROBE", `{"httpGet":{"path":"/","port":8080,"host":"127.0.0.1","scheme":"HTTP"}}`),
			),
		}, withRuntimeClass("kata")),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := revConfig()
			cfg.Observability = &test.oc
			cfg.Deployment = &test.dc
			cfg.Features = &test.fc
			if test.defaults != nil {
				cfg.Defaults = test.defaults
			}
			got, err := makePodSpec(test.rev, cfg)
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
			func(revision *v1.Revision) {
				revision.Labels = map[string]string{
					sidecarIstioInjectLabel: "false",
				}
			}),
		want: appsv1deployment(func(deploy *appsv1.Deployment) {
			deploy.Labels = kmap.Union(deploy.Labels,
				map[string]string{sidecarIstioInjectLabel: "false"})
			deploy.Spec.Template.Labels = kmap.Union(deploy.Spec.Template.Labels,
				map[string]string{sidecarIstioInjectLabel: "false"})
		}),
	}, {
		name: "with progress-deadline override",
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
		name: "with progress-deadline annotation",
		rev: revision("bar", "foo",
			WithRevisionAnn("serving.knative.dev/progress-deadline", "42s"),
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
			deploy.Annotations = map[string]string{
				serving.ProgressDeadlineAnnotationKey: "42s",
			}
			deploy.Spec.Template.Annotations = map[string]string{
				DefaultContainerAnnotationName:        servingContainerName,
				serving.ProgressDeadlineAnnotationKey: "42s",
			}
		}),
	}, {
		name: "with ProgressDeadline annotation and configmap override",
		dc: deployment.Config{
			ProgressDeadline: 503 * time.Second,
		},
		rev: revision("bar", "foo",
			WithRevisionAnn("serving.knative.dev/progress-deadline", "42s"),
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
			deploy.Annotations = map[string]string{
				serving.ProgressDeadlineAnnotationKey: "42s",
			}
			deploy.Spec.Template.Annotations = map[string]string{
				DefaultContainerAnnotationName:        servingContainerName,
				serving.ProgressDeadlineAnnotationKey: "42s",
			}
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
			deploy.Annotations = map[string]string{
				autoscaling.InitialScaleAnnotationKey: "20",
			}
			deploy.Spec.Template.Annotations = map[string]string{
				autoscaling.InitialScaleAnnotationKey: "20",
				DefaultContainerAnnotationName:        servingContainerName,
			}
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
			cfg := revConfig()
			cfg.Autoscaler = ac
			cfg.Deployment = &test.dc
			podSpec, err := makePodSpec(test.rev, cfg)
			if err != nil {
				t.Fatal("makePodSpec returned error:", err)
			}
			if test.want != nil {
				test.want.Spec.Template.Spec = *podSpec
			}
			// Copy to override
			got, err := MakeDeployment(test.rev, cfg)
			if err != nil {
				t.Fatal("Got unexpected error:", err)
			}
			if diff := cmp.Diff(test.want, got, quantityComparer); diff != "" {
				t.Errorf("MakeDeployment (-want, +got) =\n%s", diff)
			}
		})
	}
}
