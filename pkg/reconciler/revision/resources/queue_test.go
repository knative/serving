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
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap/zapcore"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	netapi "knative.dev/networking/pkg/apis/networking"
	netcfg "knative.dev/networking/pkg/config"
	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	tracingconfig "knative.dev/pkg/tracing/config"
	apicfg "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler/revision/config"

	_ "knative.dev/pkg/metrics/testing"
	_ "knative.dev/pkg/system/testing"
)

var (
	testProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
			},
		},
	}

	containers = []corev1.Container{{
		Name:           servingContainerName,
		Image:          "busybox",
		ReadinessProbe: withTCPReadinessProbe(v1.DefaultUserPort),
	}}

	// The default CM values.
	asConfig = autoscalerconfig.Config{
		InitialScale:          1,
		AllowZeroInitialScale: false,
	}
	deploymentConfig deployment.Config
	logConfig        logging.Config
	obsConfig        metrics.ObservabilityConfig
	traceConfig      tracingconfig.Config
	defaults, _      = apicfg.NewDefaultsConfigFromMap(nil)
)

const testProbeJSONTemplate = `{"tcpSocket":{"port":%d,"host":"127.0.0.1"}}`

func TestMakeQueueContainer(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		lc   logging.Config
		nc   netcfg.Config
		oc   metrics.ObservabilityConfig
		dc   deployment.Config
		fc   apicfg.Features
		want corev1.Container
	}{{
		name: "autoscaler single",
		rev: revision("bar", "foo",
			withContainers(containers),
			withContainerConcurrency(1)),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY": "1",
			})
		}),
	}, {
		name: "custom readiness probe port",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name: servingContainerName,
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Host: "127.0.0.1",
							Port: intstr.FromInt(8087),
						},
					},
				},
				Ports: []corev1.ContainerPort{{
					ContainerPort: 1955,
					Name:          string(netapi.ProtocolH2C),
				}},
			}})),
		dc: deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Image = "alpine"
			c.Ports = append(queueNonServingPorts, queueHTTP2Port, queueHTTPSPort)
			c.ReadinessProbe.ProbeHandler.HTTPGet.Port.IntVal = queueHTTP2Port.ContainerPort
			c.Env = env(map[string]string{
				"USER_PORT":          "1955",
				"QUEUE_SERVING_PORT": "8013",
			})
		}),
	}, {
		name: "custom sidecar image, container port, protocol",
		rev: revision("bar", "foo",
			withContainers([]corev1.Container{{
				Name:           servingContainerName,
				ReadinessProbe: testProbe,
				Ports: []corev1.ContainerPort{{
					ContainerPort: 1955,
					Name:          string(netapi.ProtocolH2C),
				}},
			}})),
		dc: deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Image = "alpine"
			c.Ports = append(queueNonServingPorts, queueHTTP2Port, queueHTTPSPort)
			c.ReadinessProbe.ProbeHandler.HTTPGet.Port.IntVal = queueHTTP2Port.ContainerPort
			c.Env = env(map[string]string{
				"USER_PORT":          "1955",
				"QUEUE_SERVING_PORT": "8013",
			})
		}),
	}, {
		name: "service name in labels",
		rev: revision("bar", "foo",
			withContainers(containers),
			func(revision *v1.Revision) {
				revision.Labels = map[string]string{
					serving.ServiceLabelKey: "svc",
				}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_SERVICE": "svc",
			})
		}),
	}, {
		name: "config owner as env var, zero concurrency",
		rev: revision("blah", "baz",
			withContainers(containers),
			withContainerConcurrency(0),
			func(revision *v1.Revision) {
				revision.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "the-parent-config-name",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY": "0",
				"SERVING_CONFIGURATION": "the-parent-config-name",
				"SERVING_NAMESPACE":     "baz",
				"SERVING_REVISION":      "blah",
			})
		}),
	}, {
		name: "logging configuration as env var",
		rev: revision("this", "log",
			withContainers(containers)),
		lc: logging.Config{
			LoggingConfig: "The logging configuration goes here",
			LoggingLevel: map[string]zapcore.Level{
				"queueproxy": zapcore.ErrorLevel,
			},
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_LOGGING_CONFIG": "The logging configuration goes here",
				"SERVING_LOGGING_LEVEL":  "error",
				"SERVING_NAMESPACE":      "log",
				"SERVING_REVISION":       "this",
			})
		}),
	}, {
		name: "container concurrency 10",
		rev: revision("bar", "foo",
			withContainers(containers),
			withContainerConcurrency(10)),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"CONTAINER_CONCURRENCY": "10",
			})
		}),
	}, {
		name: "request log configuration as env var",
		rev: revision("bar", "foo",
			withContainers(containers)),
		oc: metrics.ObservabilityConfig{
			RequestLogTemplate:    "test template",
			EnableProbeRequestLog: true,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_REQUEST_LOG_TEMPLATE":     "test template",
				"SERVING_ENABLE_PROBE_REQUEST_LOG": "true",
			})
		}),
	}, {
		name: "disabled request log configuration as env var",
		rev: revision("bar", "foo",
			withContainers(containers)),
		oc: metrics.ObservabilityConfig{
			RequestLogTemplate:    "test template",
			EnableProbeRequestLog: false,
			EnableRequestLog:      false,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_REQUEST_LOG_TEMPLATE":     "test template",
				"SERVING_ENABLE_REQUEST_LOG":       "false",
				"SERVING_ENABLE_PROBE_REQUEST_LOG": "false",
			})
		}),
	}, {
		name: "request metrics backend as env var",
		rev: revision("bar", "foo",
			withContainers(containers)),
		oc: metrics.ObservabilityConfig{
			RequestMetricsBackend: "prometheus",
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_REQUEST_METRICS_BACKEND": "prometheus",
			})
		}),
	}, {
		name: "enable profiling",
		rev: revision("bar", "foo",
			withContainers(containers)),
		oc: metrics.ObservabilityConfig{EnableProfiling: true},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"ENABLE_PROFILING": "true",
			})
			c.Ports = append(queueNonServingPorts, profilingPort, queueHTTPPort, queueHTTPSPort)
		}),
	}, {
		name: "custom TimeoutSeconds",
		rev: revision("bar", "foo",
			withContainers(containers),
			func(revision *v1.Revision) {
				revision.Spec.TimeoutSeconds = ptr.Int64(99)
			},
		),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"REVISION_TIMEOUT_SECONDS": "99",
			})
		}),
	}, {
		name: "custom ResponseStartTimeoutSeconds",
		rev: revision("bar", "foo",
			withContainers(containers),
			func(revision *v1.Revision) {
				revision.Spec.ResponseStartTimeoutSeconds = ptr.Int64(77)
			},
		),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"REVISION_RESPONSE_START_TIMEOUT_SECONDS": "77",
			})
		}),
	}, {
		name: "custom IdleTimeoutSeconds",
		rev: revision("bar", "foo",
			withContainers(containers),
			func(revision *v1.Revision) {
				revision.Spec.IdleTimeoutSeconds = ptr.Int64(99)
			},
		),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"REVISION_IDLE_TIMEOUT_SECONDS": "99",
			})
		}),
	}, {
		name: "default resource config with feature qp defaults disabled",
		rev: revision("bar", "foo",
			withContainers(containers)),
		dc: deployment.Config{
			QueueSidecarCPURequest: &deployment.QueueSidecarCPURequestDefault,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU: deployment.QueueSidecarCPURequestDefault,
			}
		}),
	}, {
		name: "resource config with feature qp defaults enabled",
		rev: revision("bar", "foo",
			withContainers(containers)),
		dc: deployment.Config{
			QueueSidecarCPURequest: &deployment.QueueSidecarCPURequestDefault,
		},
		fc: apicfg.Features{
			QueueProxyResourceDefaults: apicfg.Enabled,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    deployment.QueueSidecarCPURequestDefault,
				corev1.ResourceMemory: deployment.QueueSidecarMemoryRequestDefault,
			}
			c.Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:    deployment.QueueSidecarCPULimitDefault,
				corev1.ResourceMemory: deployment.QueueSidecarMemoryLimitDefault,
			}
		}),
	}, {
		name: "overridden resources",
		rev: revision("bar", "foo",
			withContainers(containers)),
		dc: deployment.Config{
			QueueSidecarCPURequest:              resourcePtr(resource.MustParse("123m")),
			QueueSidecarEphemeralStorageRequest: resourcePtr(resource.MustParse("456M")),
			QueueSidecarMemoryLimit:             resourcePtr(resource.MustParse("789m")),
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("123m"),
				corev1.ResourceEphemeralStorage: resource.MustParse("456M"),
			}
			c.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("789m"),
			}
		}),
	}, {
		name: "collector address as env var",
		rev: revision("bar", "foo",
			withContainers(containers)),
		oc: metrics.ObservabilityConfig{
			RequestMetricsBackend:   "opencensus",
			MetricsCollectorAddress: "otel:55678",
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"SERVING_REQUEST_METRICS_BACKEND": "opencensus",
				"METRICS_COLLECTOR_ADDRESS":       "otel:55678",
			})
		}),
	}, {
		name: "HTTP2 autodetection enabled",
		rev: revision("bar", "foo",
			withContainers(containers)),
		fc: apicfg.Features{
			AutoDetectHTTP2: apicfg.Enabled,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"ENABLE_HTTP2_AUTO_DETECTION": "true",
			})
		}),
	}, {
		name: "HTTP1 full duplex enabled",
		rev: revision("bar", "foo",
			withContainers(containers),
			WithRevisionAnnotations(map[string]string{apicfg.AllowHTTPFullDuplexFeatureKey: string(apicfg.Enabled)})),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"ENABLE_HTTP_FULL_DUPLEX": "true",
			})
		}),
	}, {
		name: "set root ca",
		rev: revision("bar", "foo",
			withContainers(containers)),
		dc: deployment.Config{
			QueueSidecarRootCA: "xyz",
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"ROOT_CA": "xyz",
			})
		}),
	}, {
		name: "HTTP2 autodetection disabled",
		rev: revision("bar", "foo",
			withContainers(containers)),
		fc: apicfg.Features{
			AutoDetectHTTP2: apicfg.Disabled,
		},
		dc: deployment.Config{
			ProgressDeadline: 0 * time.Second,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{
				"ENABLE_HTTP2_AUTO_DETECTION": "false",
			})
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.rev.Spec.PodSpec.Containers) == 0 {
				test.rev.Spec.PodSpec = corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           servingContainerName,
						ReadinessProbe: testProbe,
					}},
				}
			}
			cfg := &config.Config{
				Tracing:       &traceConfig,
				Logging:       &test.lc,
				Observability: &test.oc,
				Deployment:    &test.dc,
				Config: &apicfg.Config{
					Features: &test.fc,
				},
			}
			got, err := makeQueueContainer(test.rev, cfg)
			if err != nil {
				t.Fatal("makeQueueContainer returned error:", err)
			}

			test.want.Env = append(test.want.Env, corev1.EnvVar{
				Name:  "SERVING_READINESS_PROBE",
				Value: probeJSON(test.rev.Spec.GetContainer()),
			})

			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if got, want := *got, test.want; !cmp.Equal(got, want, quantityComparer) {
				t.Errorf("makeQueueContainer (-want, +got) =\n%s", cmp.Diff(want, got, quantityComparer))
			}
		})
	}
}

func TestMakeQueueContainerWithPercentageAnnotation(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		want corev1.Container
		dc   deployment.Config
	}{{
		name: "resources percentage in annotations",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarResourcePercentageAnnotationKey: "20",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
							corev1.ResourceCPU:    resource.MustParse("2"),
						},
					}},
				}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("0.4Gi"),
				corev1.ResourceCPU:    resource.MustParse("0.4"),
			}
		}),
	}, {
		name: "resources percentage in annotations smaller than min allowed",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarResourcePercentageAnnotationKey: "0.2",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				}}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("25m"), // clamped to boundary in resourceboundary.go
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			}
		}),
	}, {
		name: "invalid resources percentage in annotations uses defaults",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarResourcePercentageAnnotationKey: "foo",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				}}
			}),
		dc: deployment.Config{
			QueueSidecarCPURequest: resourcePtr(resource.MustParse("25m")),
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("25m"),
			}
		}),
	}, {
		name: "resources percentage in annotations bigger than than math.MaxInt64",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarResourcePercentageAnnotationKey: "100",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("900000Pi"),
						},
					},
				}}
			}),
		dc: deployment.Config{
			QueueSidecarCPURequest: resourcePtr(resource.MustParse("25m")),
		},
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("25m"),
				corev1.ResourceMemory: resource.MustParse("200Mi"),
			}
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := revConfig()
			cfg.Deployment = &test.dc
			got, err := makeQueueContainer(test.rev, cfg)
			if err != nil {
				t.Fatal("makeQueueContainer returned error:", err)
			}
			test.want.Env = append(test.want.Env, corev1.EnvVar{
				Name:  "SERVING_READINESS_PROBE",
				Value: probeJSON(test.rev.Spec.GetContainer()),
			})
			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if got, want := *got, test.want; !cmp.Equal(got, want, quantityComparer) {
				t.Errorf("makeQueueContainer (-want, +got) =\n%s", cmp.Diff(want, got, quantityComparer))
			}
		})
	}
}

func TestMakeQueueContainerWithResourceAnnotations(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		want corev1.Container
		dc   deployment.Config
	}{{
		name: "resources defined via annotations",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarCPUResourceRequestAnnotationKey:              "1",
					serving.QueueSidecarCPUResourceLimitAnnotationKey:                "2",
					serving.QueueSidecarMemoryResourceRequestAnnotationKey:           "1Gi",
					serving.QueueSidecarMemoryResourceLimitAnnotationKey:             "2Gi",
					serving.QueueSidecarEphemeralStorageResourceRequestAnnotationKey: "500Mi",
					serving.QueueSidecarEphemeralStorageResourceLimitAnnotationKey:   "600Mi",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
				}}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory:           resource.MustParse("1Gi"),
				corev1.ResourceCPU:              resource.MustParse("1"),
				corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"),
			}
			c.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory:           resource.MustParse("2Gi"),
				corev1.ResourceCPU:              resource.MustParse("2"),
				corev1.ResourceEphemeralStorage: resource.MustParse("600Mi"),
			}
		}),
	}, {
		name: "resources defined via annotations with bad values ignored",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarCPUResourceRequestAnnotationKey:    "zzz",
					serving.QueueSidecarCPUResourceLimitAnnotationKey:      "2",
					serving.QueueSidecarMemoryResourceRequestAnnotationKey: "Gdx",
					serving.QueueSidecarMemoryResourceLimitAnnotationKey:   "2Gi",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
				}}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),
				corev1.ResourceCPU:    resource.MustParse("2"),
			}
		}),
	}, {
		name: "resources defined via annotations mixed with percentage annotation",
		rev: revision("bar", "foo",
			func(revision *v1.Revision) {
				revision.Annotations = map[string]string{
					serving.QueueSidecarCPUResourceLimitAnnotationKey:    "1",
					serving.QueueSidecarMemoryResourceLimitAnnotationKey: "4Gi",
					serving.QueueSidecarResourcePercentageAnnotationKey:  "50",
				}
				revision.Spec.PodSpec.Containers = []corev1.Container{{
					Name:           servingContainerName,
					ReadinessProbe: testProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
							corev1.ResourceCPU:    resource.MustParse("1"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
							corev1.ResourceCPU:    resource.MustParse("2"),
						},
					}},
				}
			}),
		want: queueContainer(func(c *corev1.Container) {
			c.Env = env(map[string]string{})
			c.Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("200Mi"), // hits the boundary for max value
				corev1.ResourceCPU:    resource.MustParse("100m"),  // hits the boundary for max value
			}
			c.Resources.Limits = corev1.ResourceList{ // enforce the desired limits
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			}
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := revConfig()
			cfg.Deployment = &test.dc
			got, err := makeQueueContainer(test.rev, cfg)
			if err != nil {
				t.Fatal("makeQueueContainer returned error:", err)
			}
			test.want.Env = append(test.want.Env, corev1.EnvVar{
				Name:  "SERVING_READINESS_PROBE",
				Value: probeJSON(test.rev.Spec.GetContainer()),
			})
			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if got, want := *got, test.want; !cmp.Equal(got, want, quantityComparer) {
				t.Errorf("makeQueueContainer (-want, +got) =\n%s", cmp.Diff(want, got, quantityComparer))
			}
		})
	}
}

func TestProbeGenerationHTTPDefaults(t *testing.T) {
	rev := revision("bar", "foo",
		func(revision *v1.Revision) {
			revision.Spec.PodSpec.Containers = []corev1.Container{{
				Name: servingContainerName,
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
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
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Path:   "/",
				Port:   intstr.FromInt(int(v1.DefaultUserPort)),
				Scheme: corev1.URISchemeHTTP,
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  netheader.KubeletProbeKey,
					Value: queue.Name,
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

	want := queueContainer(func(c *corev1.Container) {
		c.Env = env(map[string]string{
			"SERVING_READINESS_PROBE": string(wantProbeJSON),
		})
		c.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(int(queueHTTPPort.ContainerPort)),
					HTTPHeaders: []corev1.HTTPHeader{{
						Name:  netheader.ProbeKey,
						Value: queue.Name,
					}},
				},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 10,
		}
	})

	got, err := makeQueueContainer(rev, revConfig())
	if err != nil {
		t.Fatal("makeQueueContainer returned error")
	}
	sortEnv(got.Env)
	if got, want := *got, want; !cmp.Equal(got, want, quantityComparer) {
		t.Errorf("makeQueueContainer(-want, +got) =\n%s", cmp.Diff(want, got, quantityComparer))
	}
}

func TestProbeGenerationHTTP(t *testing.T) {
	const userPort = 12345
	const probePath = "/health"

	rev := revision("bar", "foo",
		func(revision *v1.Revision) {
			revision.Spec.PodSpec.Containers = []corev1.Container{{
				Name: servingContainerName,
				Ports: []corev1.ContainerPort{{
					ContainerPort: userPort,
				}},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
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
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Path:   probePath,
				Port:   intstr.FromInt(userPort),
				Scheme: corev1.URISchemeHTTPS,
				HTTPHeaders: []corev1.HTTPHeader{{
					Name:  netheader.KubeletProbeKey,
					Value: queue.Name,
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

	want := queueContainer(func(c *corev1.Container) {
		c.Env = env(map[string]string{
			"USER_PORT":               strconv.Itoa(userPort),
			"SERVING_READINESS_PROBE": string(wantProbeJSON),
		})
		c.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(int(queueHTTPPort.ContainerPort)),
					HTTPHeaders: []corev1.HTTPHeader{{
						Name:  netheader.ProbeKey,
						Value: queue.Name,
					}},
				},
			},
			PeriodSeconds:  2,
			TimeoutSeconds: 10,
		}
	})

	got, err := makeQueueContainer(rev, revConfig())
	if err != nil {
		t.Fatal("makeQueueContainer returned error")
	}
	sortEnv(got.Env)
	if got, want := *got, want; !cmp.Equal(got, want, quantityComparer) {
		t.Errorf("makeQueueContainer(-want, +got) =\n%s", cmp.Diff(want, got, quantityComparer))
	}
}

func TestTCPProbeGeneration(t *testing.T) {
	const userPort = 12345
	tests := []struct {
		name      string
		rev       v1.RevisionSpec
		want      corev1.Container
		wantProbe *corev1.Probe
		dc        deployment.Config
	}{{
		name: "knative tcp probe",
		wantProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Host: "127.0.0.1",
					Port: intstr.FromInt(userPort),
				},
			},
			PeriodSeconds:    0,
			SuccessThreshold: 3,
		},
		rev: v1.RevisionSpec{
			TimeoutSeconds: ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: servingContainerName,
					Ports: []corev1.ContainerPort{{
						ContainerPort: userPort,
					}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{},
						},
						PeriodSeconds:    0,
						SuccessThreshold: 3,
					},
				}},
			},
		},
		dc: deployment.Config{
			ProgressDeadline: 5678 * time.Second,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.ReadinessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(int(queueHTTPPort.ContainerPort)),
						HTTPHeaders: []corev1.HTTPHeader{{
							Name:  netheader.ProbeKey,
							Value: queue.Name,
						}},
					},
				},
				PeriodSeconds:    0,
				TimeoutSeconds:   0,
				SuccessThreshold: 3,
			}
			c.Env = env(map[string]string{"USER_PORT": strconv.Itoa(userPort)})
		}),
	}, {
		name: "tcp defaults",
		rev: v1.RevisionSpec{
			TimeoutSeconds: ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: servingContainerName,
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{},
						},
						PeriodSeconds: 1,
					},
				}},
			},
		},
		wantProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Host: "127.0.0.1",
					Port: intstr.FromInt(int(v1.DefaultUserPort)),
				},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1,
		},
		want: queueContainer(func(c *corev1.Container) {
			c.ReadinessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(int(queueHTTPPort.ContainerPort)),
						HTTPHeaders: []corev1.HTTPHeader{{
							Name:  netheader.ProbeKey,
							Value: queue.Name,
						}},
					},
				},
				PeriodSeconds: 1,
				// Inherit Kubernetes default here rather than overriding as we need to do for exec probe.
				TimeoutSeconds: 0,
			}
			c.Env = env(map[string]string{})
		}),
	}, {
		name: "user defined tcp probe",
		wantProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
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
			TimeoutSeconds: ptr.Int64(45),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: servingContainerName,
					Ports: []corev1.ContainerPort{{
						ContainerPort: userPort,
					}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
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
		want: queueContainer(func(c *corev1.Container) {
			c.ReadinessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(int(queueHTTPPort.ContainerPort)),
						HTTPHeaders: []corev1.HTTPHeader{{
							Name:  netheader.ProbeKey,
							Value: queue.Name,
						}},
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
			testRev := revision("bar", "foo",
				func(revision *v1.Revision) {
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

			config := revConfig()
			config.Deployment = &test.dc

			got, err := makeQueueContainer(testRev, config)
			if err != nil {
				t.Fatal("makeQueueContainer returned error")
			}
			sortEnv(got.Env)
			sortEnv(test.want.Env)
			if got, want := *got, test.want; !cmp.Equal(want, got, quantityComparer) {
				t.Errorf("makeQueueContainer (-want, +got) =\n%s", cmp.Diff(want, got, quantityComparer))
			}
		})
	}
}

var defaultEnv = map[string]string{
	"CONTAINER_CONCURRENCY":                            "0",
	"ENABLE_HTTP2_AUTO_DETECTION":                      "false",
	"ENABLE_HTTP_FULL_DUPLEX":                          "false",
	"ENABLE_PROFILING":                                 "false",
	"METRICS_DOMAIN":                                   metrics.Domain(),
	"METRICS_COLLECTOR_ADDRESS":                        "",
	"QUEUE_SERVING_PORT":                               "8012",
	"QUEUE_SERVING_TLS_PORT":                           "8112",
	"REVISION_TIMEOUT_SECONDS":                         "45",
	"REVISION_RESPONSE_START_TIMEOUT_SECONDS":          "0",
	"REVISION_IDLE_TIMEOUT_SECONDS":                    "0",
	"SERVING_CONFIGURATION":                            "",
	"SERVING_ENABLE_PROBE_REQUEST_LOG":                 "false",
	"SERVING_ENABLE_REQUEST_LOG":                       "false",
	"SERVING_LOGGING_CONFIG":                           "",
	"SERVING_LOGGING_LEVEL":                            "",
	"SERVING_NAMESPACE":                                "foo",
	"SERVING_REQUEST_LOG_TEMPLATE":                     "",
	"SERVING_REQUEST_METRICS_BACKEND":                  "",
	"SERVING_REQUEST_METRICS_REPORTING_PERIOD_SECONDS": "0",
	"SERVING_REVISION":                                 "bar",
	"SERVING_SERVICE":                                  "",
	"SYSTEM_NAMESPACE":                                 system.Namespace(),
	"TRACING_CONFIG_BACKEND":                           "",
	"TRACING_CONFIG_DEBUG":                             "false",
	"TRACING_CONFIG_SAMPLE_RATE":                       "0",
	"TRACING_CONFIG_ZIPKIN_ENDPOINT":                   "",
	"USER_PORT":                                        strconv.Itoa(v1.DefaultUserPort),
	"ROOT_CA":                                          "",
}

func probeJSON(container *corev1.Container) string {
	if container == nil {
		return fmt.Sprintf(testProbeJSONTemplate, v1.DefaultUserPort)
	}
	if container.ReadinessProbe.TCPSocket != nil && container.ReadinessProbe.TCPSocket.Port.IntValue() != 0 {
		return fmt.Sprintf(testProbeJSONTemplate, container.ReadinessProbe.TCPSocket.Port.IntVal)
	}
	if container.ReadinessProbe.HTTPGet != nil && container.ReadinessProbe.HTTPGet.Port.IntValue() != 0 {
		return fmt.Sprintf(testProbeJSONTemplate, container.ReadinessProbe.HTTPGet.Port.IntVal)
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
		Name: "HOST_IP",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "status.hostIP"},
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

func resourcePtr(q resource.Quantity) *resource.Quantity {
	return &q
}

func revConfig() *config.Config {
	return &config.Config{
		Config: &apicfg.Config{
			Autoscaler: &asConfig,
			Defaults:   defaults,
			Features:   &apicfg.Features{},
		},
		Deployment:    &deploymentConfig,
		Logging:       &logConfig,
		Network:       &netcfg.Config{},
		Observability: &obsConfig,
		Tracing:       &traceConfig,
	}
}
