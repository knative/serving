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
	"encoding/json"
	"fmt"
	"knative.dev/serving/pkg/apis/networking"
	"sort"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/zap/zapcore"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/network"
)

var (
	defaultKnativeQReadinessProbe = &corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{
				Command: []string{"/ko-app/queue", "-probe-period", "0"},
			},
		},
		// We want to mark the service as not ready as soon as the
		// PreStop handler is called, so we need to check a little
		// bit more often than the default.  It is a small
		// sacrifice for a low rate of 503s.
		PeriodSeconds: 1,
		// We keep the connection open for a while because we're
		// actively probing the user-container on that endpoint and
		// thus don't want to be limited by K8s granularity here.
		TimeoutSeconds: 10,
	}
	testProbe = &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
			},
		},
	}

	// The default CM values.
	logConfig        logging.Config
	traceConfig      tracingconfig.Config
	obsConfig        metrics.ObservabilityConfig
	asConfig         autoscalerconfig.Config
	deploymentConfig deployment.Config
)

const testProbeJSONTemplate = `{"tcpSocket":{"port":%d,"host":"127.0.0.1"}}`

func TestMakeQueueContainer(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		lc   logging.Config
		oc   metrics.ObservabilityConfig
		cc   deployment.Config
		want corev1.Container
	}{{
		name: "no owner no autoscaler single",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY": "1",
			})
		}),
	}, {
		name: "no owner no autoscaler single",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
				}
				revision.Spec.PodSpec = corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           containerName,
						ReadinessProbe: testProbe,
						Ports: []corev1.ContainerPort{{
							ContainerPort: 1955,
							Name:          string(networking.ProtocolH2C),
						}},
					}},
				}
			}),
		cc: deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Image = "alpine"
				c.Ports = append(queueNonServingPorts, queueHTTP2Port)
				c.Env = env(map[string]string{
					"USER_PORT":             "1955",
					"QUEUE_SERVING_PORT":    "8013",
					"CONTAINER_CONCURRENCY": "1",
				})
			},
		),
	}, {
		name: "service name in labels",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
					Labels: map[string]string{
						serving.ServiceLabelKey: "svc",
					},
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		cc: deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
					"SERVING_SERVICE": "svc",
				})
				c.Image = "alpine"
				c.Ports = append(queueNonServingPorts, queueHTTPPort)
			}),
	}, {
		name: "config owner as env var, zero concurrency",
		rev: revision(
			withContainerConcurrency(0),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "baz",
					Name:      "blah",
					UID:       "1234",
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         v1.SchemeGroupVersion.String(),
						Kind:               "Configuration",
						Name:               "the-parent-config-name",
						Controller:         ptr.Bool(true),
						BlockOwnerDeletion: ptr.Bool(true),
					}},
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY": "0",
				"SERVING_CONFIGURATION": "the-parent-config-name",
				"SERVING_NAMESPACE":     "baz",
				"SERVING_REVISION":      "blah",
				})
				c.Ports = append(queueNonServingPorts, queueHTTPPort)
			}),
	}, {
		name: "logging configuration as env var",
		rev: revision(
			withContainerConcurrency(0),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "log",
					Name:      "this",
					UID:       "1234",
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		lc: logging.Config{
			LoggingConfig: "The logging configuration goes here",
			LoggingLevel: map[string]zapcore.Level{
				"queueproxy": zapcore.ErrorLevel,
			},
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY":  "0",
				"SERVING_LOGGING_CONFIG": "The logging configuration goes here",
				"SERVING_LOGGING_LEVEL":  "error",
				"SERVING_NAMESPACE":      "log",
				"SERVING_REVISION":       "this",
				})
				c.Ports = append(queueNonServingPorts, queueHTTPPort)
			}),
	}, {
		name: "container concurrency 10",
		rev: revision(
			withContainerConcurrency(10),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY": "10",
				})
				c.Ports = append(queueNonServingPorts, queueHTTPPort)
			}),
	}, {
		name: "request log configuration as env var",
		rev: revision(
			withContainerConcurrency(0),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		oc: metrics.ObservabilityConfig{
			RequestLogTemplate:    "test template",
			EnableProbeRequestLog: true,
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY":            "0",
				"SERVING_REQUEST_LOG_TEMPLATE":     "test template",
				"SERVING_ENABLE_PROBE_REQUEST_LOG": "true",
				})
				c.Ports = append(queueNonServingPorts, queueHTTPPort)
			}),
	}, {
		name: "request metrics backend as env var",
		rev: revision(
			withContainerConcurrency(0),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		oc: metrics.ObservabilityConfig{
			RequestMetricsBackend: "prometheus",
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY":           "0",
				"SERVING_REQUEST_METRICS_BACKEND": "prometheus",
				})
				c.Ports = append(queueNonServingPorts, queueHTTPPort)
			}),
	}, {
		name: "enable profiling",
		rev: revision(
			withContainerConcurrency(0),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
				}
				container(revision.Spec.GetContainer(), withTCPReadinessProbe())
			}),
		oc: metrics.ObservabilityConfig{EnableProfiling: true},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
					"CONTAINER_CONCURRENCY": "0",
					"ENABLE_PROFILING":      "true",
				})
				c.Ports = append(queueNonServingPorts, profilingPort, queueHTTPPort)
			}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.rev.Spec.PodSpec.Containers) == 0 {
				test.rev.Spec.PodSpec = corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           containerName,
						ReadinessProbe: testProbe,
					}},
				}
			}
			got, err := makeQueueContainer(test.rev, &test.lc, &traceConfig, &test.oc, &asConfig, &test.cc)
			if err != nil {
				t.Fatalf("makeQueueContainer returned error: %v", err)
			}

			test.want.Env = append(test.want.Env, corev1.EnvVar{
				Name:  "SERVING_READINESS_PROBE",
				Value: probeJSON(test.rev.Spec.GetContainer()),
			})
			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if diff := cmp.Diff(test.want, *got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeQueueContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeQueueContainerWithPercentageAnnotation(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		want corev1.Container
	}{{
		name: "resources percentage in annotations",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
					Labels: map[string]string{
						serving.ServiceLabelKey: "svc",
					},
					Annotations: map[string]string{
						serving.QueueSideCarResourcePercentageAnnotation: "20",
					},
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           containerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceName("memory"): resource.MustParse("2Gi"),
							corev1.ResourceName("cpu"):    resource.MustParse("2"),
						},
					}},
				}
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
					"SERVING_SERVICE": "svc",
				})
				c.Resources.Limits = corev1.ResourceList{
					corev1.ResourceName("memory"): *resource.NewMilliQuantity(429496729600, resource.BinarySI),
					corev1.ResourceName("cpu"):    *resource.NewMilliQuantity(400, resource.BinarySI),
				}
				c.Image = "alpine"
			}),
	}, {
		name: "resources percentage in annotations small than min allowed",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
					Labels: map[string]string{
						serving.ServiceLabelKey: "svc",
					},
					Annotations: map[string]string{
						serving.QueueSideCarResourcePercentageAnnotation: "0.2",
					},
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           containerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("cpu"):    resource.MustParse("50m"),
							corev1.ResourceName("memory"): resource.MustParse("128Mi"),
						},
					},
				}}
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
					"SERVING_SERVICE": "svc",
				})
				c.Resources.Requests = corev1.ResourceList{
					corev1.ResourceName("cpu"):    resource.MustParse("25m"),
					corev1.ResourceName("memory"): resource.MustParse("50Mi"),
				}
				c.Image = "alpine"
			}),
	}, {
		name: "Invalid resources percentage in annotations",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
					Labels: map[string]string{
						serving.ServiceLabelKey: "svc",
					},
					Annotations: map[string]string{
						serving.QueueSideCarResourcePercentageAnnotation: "foo",
					},
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           containerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("cpu"):    resource.MustParse("50m"),
							corev1.ResourceName("memory"): resource.MustParse("128Mi"),
						},
					},
				}}
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
					"SERVING_SERVICE": "svc",
				})
				c.Resources.Requests = corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				}
				c.Image = "alpine"
			}),
	}, {
		name: "resources percentage in annotations bigger than than math.MaxInt64",
		rev: revision(
			withContainerConcurrency(1),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(45)
				revision.ObjectMeta = metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					UID:       "1234",
					Labels: map[string]string{
						serving.ServiceLabelKey: "svc",
					},
					Annotations: map[string]string{
						serving.QueueSideCarResourcePercentageAnnotation: "100",
					},
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           containerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("memory"): resource.MustParse("900000Pi"),
						},
					},
				}}
			}),
		want: queueContainer(
			func(c *corev1.Container) {
				c.Env = env(map[string]string{
					"SERVING_SERVICE": "svc",
				})
				c.Resources.Requests = corev1.ResourceList{
					corev1.ResourceName("cpu"):    resource.MustParse("25m"),
					corev1.ResourceName("memory"): resource.MustParse("200Mi"),
				}
				c.Image = "alpine"
			}),
	}}

	cc := deployment.Config{QueueSidecarImage: "alpine"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := makeQueueContainer(test.rev, &logConfig, &traceConfig, &obsConfig, &asConfig, &cc)
			if err != nil {
				t.Fatalf("makeQueueContainer returned error: %v", err)
			}
			test.want.Env = append(test.want.Env, corev1.EnvVar{
				Name:  "SERVING_READINESS_PROBE",
				Value: probeJSON(test.rev.Spec.GetContainer()),
			})
			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if diff := cmp.Diff(test.want, *got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
			}
			if test.want.Resources.Limits.Memory().Cmp(*got.Resources.Limits.Memory()) != 0 {
				t.Errorf("Resources.Limits.Memory = %v, want: %v", got.Resources.Limits.Memory(), test.want.Resources.Limits.Memory())
			}
			if test.want.Resources.Requests.Cpu().Cmp(*got.Resources.Requests.Cpu()) != 0 {
				t.Errorf("Resources.Request.CPU = %v, want: %v", got.Resources.Requests.Cpu(), test.want.Resources.Requests.Cpu())
			}
			if test.want.Resources.Requests.Memory().Cmp(*got.Resources.Requests.Memory()) != 0 {
				t.Errorf("Resources.Requests.Memory = %v, want: %v", got.Resources.Requests.Memory(), test.want.Resources.Requests.Memory())
			}
			if test.want.Resources.Limits.Cpu().Cmp(*got.Resources.Limits.Cpu()) != 0 {
				t.Errorf("Resources.Limits.CPU  = %v, want: %v", got.Resources.Limits.Cpu(), test.want.Resources.Limits.Cpu())
			}
		})
	}
}

func TestProbeGenerationHTTPDefaults(t *testing.T) {
	rev := revision(
		withContainerConcurrency(1),
		func(revision *v1.Revision) {
			revision.Spec.TimeoutSeconds = ptr.Int64(45)
			revision.ObjectMeta = metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			}
			revision.Spec.PodSpec.Containers = []corev1.Container{{
				Name: containerName,
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
					PeriodSeconds:  1,
					TimeoutSeconds: 10,
				},
			}}
		})

	expectedProbe := &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Path:   "/",
				Port:   intstr.FromInt(int(v1.DefaultUserPort)),
				Scheme: corev1.URISchemeHTTP,
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  network.KubeletProbeHeaderName,
					Value: "queue",
				}},
			},
		},
		PeriodSeconds:  1,
		TimeoutSeconds: 10,
	}

	wantProbeJSON, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatal("failed to marshal expected probe")
	}

	want := queueContainer(
		func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_READINESS_PROBE": string(wantProbeJSON),
			})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse("25m"),
			}
			c.ReadinessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					Exec: &corev1.ExecAction{
						Command: []string{"/ko-app/queue", "-probe-period", "10"},
					},
				},
				PeriodSeconds:  1,
				TimeoutSeconds: 10,
			}
		},
	)

	got, err := makeQueueContainer(rev, &logConfig, &traceConfig, &obsConfig, &asConfig, &deploymentConfig)
	if err != nil {
		t.Fatal("makeQueueContainer returned error")
	}
	sortEnv(got.Env)
	if diff := cmp.Diff(want, *got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainer(-want, +got) = %v", diff)
	}
}

func TestProbeGenerationHTTP(t *testing.T) {
	const userPort = 12345
	const probePath = "/health"

	rev := revision(
		withContainerConcurrency(1),
		func(revision *v1.Revision) {
			revision.Spec.TimeoutSeconds = ptr.Int64(45)
			revision.ObjectMeta = metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			}
			revision.Spec.PodSpec.Containers = []corev1.Container{{
				Name: containerName,
				Ports: []corev1.ContainerPort{{
					ContainerPort: int32(userPort),
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   probePath,
							Scheme: corev1.URISchemeHTTPS,
						},
					},
					PeriodSeconds:  2,
					TimeoutSeconds: 10,
				},
			}}
		})

	expectedProbe := &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Path:   probePath,
				Port:   intstr.FromInt(userPort),
				Scheme: corev1.URISchemeHTTPS,
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  network.KubeletProbeHeaderName,
					Value: "queue",
				}},
			},
		},
		PeriodSeconds:  2,
		TimeoutSeconds: 10,
	}

	wantProbeJSON, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatal("failed to marshal expected probe")
	}

	want := queueContainer(
		func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"USER_PORT":               strconv.Itoa(userPort),
				"SERVING_READINESS_PROBE": string(wantProbeJSON),
			})
			c.ReadinessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					Exec: &corev1.ExecAction{
						Command: []string{"/ko-app/queue", "-probe-period", "10"},
				}},
				PeriodSeconds:  2,
				TimeoutSeconds: 10,
			}
		},
	)

	got, err := makeQueueContainer(rev, &logConfig, &traceConfig, &obsConfig, &asConfig, &deploymentConfig)
	if err != nil {
		t.Fatal("makeQueueContainer returned error")
	}
	sortEnv(got.Env)
	if diff := cmp.Diff(want, *got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainer(-want, +got) = %v", diff)
	}
}

func TestTCPProbeGeneration(t *testing.T) {
	const userPort = 12345
	tests := []struct {
		name      string
		rev       v1.RevisionSpec
		want      corev1.Container
		wantProbe *corev1.Probe
	}{{
		name: "knative tcp probe",
		wantProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Host: "127.0.0.1",
					Port: intstr.FromInt(userPort),
				},
			},
			PeriodSeconds:    0,
			SuccessThreshold: 3,
		},
		rev: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(1),
			TimeoutSeconds:       ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: containerName,
					Ports: []corev1.ContainerPort{{
						ContainerPort: int32(userPort),
					}},
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							TCPSocket: &corev1.TCPSocketAction{},
						},
						PeriodSeconds:    0,
						SuccessThreshold: 3,
					},
				}},
			},
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Resources.Requests = corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				}
				c.ReadinessProbe = &corev1.Probe{
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"/ko-app/queue", "-probe-period", "0"},
						},
					},
					PeriodSeconds:  1,
					TimeoutSeconds: 10,
				}
				c.Env = env(map[string]string{"USER_PORT": strconv.Itoa(userPort)})
			}),
	}, {
		name: "tcp defaults",
		rev: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(1),
			TimeoutSeconds:       ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: containerName,
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							TCPSocket: &corev1.TCPSocketAction{},
						},
						PeriodSeconds: 1,
					},
				}},
			},
		},
		wantProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Host: "127.0.0.1",
					Port: intstr.FromInt(int(v1.DefaultUserPort)),
				},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1,
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Resources.Requests = corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				}
				c.ReadinessProbe = &corev1.Probe{
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"/ko-app/queue", "-probe-period", "1"},
						},
					},
					PeriodSeconds:  1,
					TimeoutSeconds: 1,
				}
				c.Env = env(map[string]string{})
			}),
	}, {
		name: "user defined tcp probe",
		wantProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Host: "127.0.0.1",
					Port: intstr.FromInt(userPort),
				},
			},
			PeriodSeconds:       2,
			TimeoutSeconds:      15,
			SuccessThreshold:    2,
			FailureThreshold:    7,
			InitialDelaySeconds: 3,
		},
		rev: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(1),
			TimeoutSeconds:       ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: containerName,
					Ports: []corev1.ContainerPort{{
						ContainerPort: int32(userPort),
					}},
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							TCPSocket: &corev1.TCPSocketAction{},
						},
						PeriodSeconds:       2,
						TimeoutSeconds:      15,
						SuccessThreshold:    2,
						FailureThreshold:    7,
						InitialDelaySeconds: 3,
					},
				}},
			},
		},
		want: queueContainer(
			func(c *corev1.Container) {
				c.Resources.Requests = corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				}
				c.ReadinessProbe = &corev1.Probe{
					Handler: corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{"/ko-app/queue", "-probe-period", "15"},
						},
					},
					PeriodSeconds:       2,
					TimeoutSeconds:      15,
					SuccessThreshold:    2,
					FailureThreshold:    7,
					InitialDelaySeconds: 3,
				}
				c.Env = env(map[string]string{"USER_PORT": strconv.Itoa(userPort)})
			}),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRev := revision(
				func(revision *v1.Revision) {
					revision.ObjectMeta = metav1.ObjectMeta{
						Namespace: "foo",
						Name:      "bar",
						UID:       "1234",
					}
					revision.Spec = test.rev
				})
			wantProbeJSON, err := json.Marshal(test.wantProbe)
			if err != nil {
				t.Fatal("failed to marshal expected probe")
			}
			test.want.Env = append(test.want.Env, corev1.EnvVar{
				Name:  "SERVING_READINESS_PROBE",
				Value: string(wantProbeJSON),
			})

			got, err := makeQueueContainer(testRev, &logConfig, &traceConfig, &obsConfig, &asConfig, &deploymentConfig)
			if err != nil {
				t.Fatal("makeQueueContainer returned error")
			}
			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if diff := cmp.Diff(test.want, *got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeQueueContainer (-want, +got) = %v", diff)
			}
		})
	}
}

var defaultEnv = map[string]string{
	"SERVING_NAMESPACE":                     "foo",
	"SERVING_SERVICE":                       "",
	"SERVING_CONFIGURATION":                 "",
	"SERVING_REVISION":                      "bar",
	"CONTAINER_CONCURRENCY":                 "1",
	"REVISION_TIMEOUT_SECONDS":              "45",
	"SERVING_LOGGING_CONFIG":                "",
	"SERVING_LOGGING_LEVEL":                 "",
	"TRACING_CONFIG_BACKEND":                "",
	"TRACING_CONFIG_ZIPKIN_ENDPOINT":        "",
	"TRACING_CONFIG_STACKDRIVER_PROJECT_ID": "",
	"TRACING_CONFIG_SAMPLE_RATE":            "0.000000",
	"TRACING_CONFIG_DEBUG":                  "false",
	"SERVING_REQUEST_LOG_TEMPLATE":          "",
	"SERVING_REQUEST_METRICS_BACKEND":       "",
	"USER_PORT":                             strconv.Itoa(v1.DefaultUserPort),
	"SYSTEM_NAMESPACE":                      system.Namespace(),
	"METRICS_DOMAIN":                        metrics.Domain(),
	"QUEUE_SERVING_PORT":                    "8012",
	"USER_CONTAINER_NAME":                   containerName,
	"ENABLE_VAR_LOG_COLLECTION":             "false",
	"VAR_LOG_VOLUME_NAME":                   varLogVolumeName,
	"INTERNAL_VOLUME_PATH":                  internalVolumePath,
	"DOWNWARD_API_LABELS_PATH":              fmt.Sprintf("%s/%s", podInfoVolumePath, metadataLabelsPath),
	"ENABLE_PROFILING":                      "false",
	"SERVING_ENABLE_PROBE_REQUEST_LOG":      "false",
}

func probeJSON(container *corev1.Container) string {
	if container == nil {
		return fmt.Sprintf(testProbeJSONTemplate, v1.DefaultUserPort)
	}

	if ports := container.Ports; len(ports) > 0 && ports[0].ContainerPort != 0 {
		return fmt.Sprintf(testProbeJSONTemplate, ports[0].ContainerPort)
	}
	return fmt.Sprintf(testProbeJSONTemplate, v1.DefaultUserPort)
}

func env(overrides map[string]string) []corev1.EnvVar {
	values := kmeta.UnionMaps(defaultEnv, overrides)

	env := make([]corev1.EnvVar, 0, len(values)+2)
	for key, value := range values {
		env = append(env, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	env = append(env, []corev1.EnvVar{{
		Name: "SERVING_POD",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		},
	}, {
		Name: "SERVING_POD_IP",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		},
	}}...)

	sortEnv(env)
	return env
}

func sortEnv(envs []corev1.EnvVar) {
	sort.SliceStable(envs, func(i, j int) bool {
		return envs[i].Name < envs[j].Name
	})
}
