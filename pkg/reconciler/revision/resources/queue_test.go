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
	"sort"
	"strconv"
	"testing"

	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/resources"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/deployment"
	"github.com/knative/serving/pkg/metrics"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
)

var defaultKnativeQReadinessProbe = &corev1.Probe{
	Handler: corev1.Handler{
		Exec: &corev1.ExecAction{
			Command: []string{"/ko-app/queue", "-probe", "0"},
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

func TestMakeQueueContainer(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		lc   *logging.Config
		oc   *metrics.ObservabilityConfig
		ac   *autoscaler.Config
		cc   *deployment.Config
		want *corev1.Container
	}{{
		name: "no owner no autoscaler single",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Env: env(nil),
		},
	}, {
		name: "no owner no autoscaler single",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Ports: []corev1.ContainerPort{{
								ContainerPort: 1955,
								Name:          string(networking.ProtocolH2C),
							}},
						}},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTP2Port),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"USER_PORT":          "1955",
				"QUEUE_SERVING_PORT": "8013",
			}),
		},
	}, {
		name: "service name in labels",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.ServiceLabelKey: "svc",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"SERVING_SERVICE": "svc",
			}),
		}}, {
		name: "config owner as env var, zero concurrency",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "baz",
				Name:      "blah",
				UID:       "1234",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "the-parent-config-name",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY": "0",
				"SERVING_CONFIGURATION": "the-parent-config-name",
				"SERVING_NAMESPACE":     "baz",
				"SERVING_REVISION":      "blah",
			}),
		},
	}, {
		name: "logging configuration as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "log",
				Name:      "this",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{
			LoggingConfig: "The logging configuration goes here",
			LoggingLevel: map[string]zapcore.Level{
				"queueproxy": zapcore.ErrorLevel,
			},
		},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY":  "0",
				"SERVING_LOGGING_CONFIG": "The logging configuration goes here",
				"SERVING_LOGGING_LEVEL":  "error",
				"SERVING_NAMESPACE":      "log",
				"SERVING_REVISION":       "this",
			}),
		},
	}, {
		name: "container concurrency 10",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 10,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY": "10",
			}),
		},
	}, {
		name: "request log as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{RequestLogTemplate: "test template"},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY":        "0",
				"SERVING_REQUEST_LOG_TEMPLATE": "test template",
			}),
		},
	}, {
		name: "request metrics backend as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{
			RequestMetricsBackend: "prometheus",
		},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:            QueueContainerName,
			Resources:       createQueueResources(make(map[string]string), &corev1.Container{}),
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY":           "0",
				"SERVING_REQUEST_METRICS_BACKEND": "prometheus",
			}),
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.rev.Spec.PodSpec.Containers) == 0 {
				test.rev.Spec.PodSpec = corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: containerName,
					}},
				}
			}

			got := makeQueueContainer(test.rev, test.lc, test.oc, test.ac, test.cc)
			sortEnv(got.Env)
			test.want.Args = probeArgs(test.want.Args, test.rev.Spec.GetContainer().ReadinessProbe)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeQueueContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeQueueContainerWithPercentageAnnotation(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		lc   *logging.Config
		oc   *metrics.ObservabilityConfig
		ac   *autoscaler.Config
		cc   *deployment.Config
		want *corev1.Container
	}{{
		name: "resources percentage in annotations",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.ServiceLabelKey: "svc",
				},
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "20",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceName("memory"): resource.MustParse("2Gi"),
									corev1.ResourceName("cpu"):    resource.MustParse("2"),
								},
							},
						}},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name: QueueContainerName,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceName("cpu"):    *resource.NewMilliQuantity(400, resource.BinarySI),
					corev1.ResourceName("memory"): *resource.NewQuantity(429496736, resource.BinarySI),
				},
			},
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"SERVING_SERVICE": "svc",
			}),
		}}, {
		name: "resources percentage in annotations small than min allowed",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.ServiceLabelKey: "svc",
				},
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "0.2",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName("cpu"):    resource.MustParse("50m"),
									corev1.ResourceName("memory"): resource.MustParse("128Mi"),
								},
							},
						}},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name: QueueContainerName,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"):    resource.MustParse("25m"),
					corev1.ResourceName("memory"): resource.MustParse("50Mi"),
				},
			},
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"SERVING_SERVICE": "svc",
			}),
		}}, {
		name: "Invalid resources percentage in annotations",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.ServiceLabelKey: "svc",
				},
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "foo",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName("cpu"):    resource.MustParse("50m"),
									corev1.ResourceName("memory"): resource.MustParse("128Mi"),
								},
							},
						}},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name: QueueContainerName,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				},
			},
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"SERVING_SERVICE": "svc",
			}),
		}}, {
		name: "resources percentage in annotations bigger than than math.MaxInt64",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.ServiceLabelKey: "svc",
				},
				Annotations: map[string]string{
					serving.QueueSideCarResourcePercentageAnnotation: "100",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: containerName,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName("memory"): resource.MustParse("900000Pi"),
								},
							},
						}},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name: QueueContainerName,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"):    resource.MustParse("25m"),
					corev1.ResourceName("memory"): resource.MustParse("200Mi"),
				},
			},
			Ports:           append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe:  defaultKnativeQReadinessProbe,
			SecurityContext: queueSecurityContext,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"SERVING_SERVICE": "svc",
			}),
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makeQueueContainer(test.rev, test.lc, test.oc, test.ac, test.cc)
			sortEnv(got.Env)
			test.want.Args = probeArgs(test.want.Args, test.rev.Spec.GetContainer().ReadinessProbe)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
			}
			if test.want.Resources.Limits.Memory().Cmp(*got.Resources.Limits.Memory()) != 0 {
				t.Errorf("Expected Resources.Limits.Memory %v got %v ", test.want.Resources.Limits.Memory(), got.Resources.Limits.Memory())
			}
			if test.want.Resources.Requests.Cpu().Cmp(*got.Resources.Requests.Cpu()) != 0 {
				t.Errorf("Expected Resources.Request.Cpu %v got %v ", test.want.Resources.Requests.Cpu(), got.Resources.Requests.Cpu())
			}
			if test.want.Resources.Requests.Memory().Cmp(*got.Resources.Requests.Memory()) != 0 {
				t.Errorf("Expected Resources.Requests.Memory %v got %v ", test.want.Resources.Requests.Memory(), got.Resources.Requests.Memory())
			}
			if test.want.Resources.Limits.Cpu().Cmp(*got.Resources.Limits.Cpu()) != 0 {
				t.Errorf("Expected Resources.Limits.Cpu %v got %v ", test.want.Resources.Limits.Cpu(), got.Resources.Limits.Cpu())
			}
		})
	}
}

func TestProbeGenerationHTTPDefaults(t *testing.T) {
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       ptr.Int64(45),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
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
					}},
				},
			},
		},
	}

	expectedProbe := &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Host:   "127.0.0.1",
				Path:   "/",
				Port:   intstr.FromInt(int(v1alpha1.DefaultUserPort)),
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
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to marshall readiness probe %#v", err)
	}

	lc := &logging.Config{}
	oc := &metrics.ObservabilityConfig{}
	ac := &autoscaler.Config{}
	cc := &deployment.Config{}
	want := &corev1.Container{
		// These are effectively constant
		Name:      QueueContainerName,
		Resources: createQueueResources(make(map[string]string), &corev1.Container{}),
		Ports:     append(queueNonServingPorts, queueHTTPPort),
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe", "10"},
				},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 10,
		},
		// These changed based on the Revision and configs passed in.
		Env:             env(map[string]string{}),
		SecurityContext: queueSecurityContext,
		Args:            []string{"--readiness-probe", string(probeBytes)},
	}

	got := makeQueueContainer(rev, lc, oc, ac, cc)
	sortEnv(got.Env)
	if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
	}
}

func TestProbeGenerationHTTP(t *testing.T) {
	userPort := 12345
	probePath := "/health"

	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       ptr.Int64(45),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: containerName,
						Ports: []v1.ContainerPort{{
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
					}},
				},
			},
		},
	}

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
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to marshall readiness probe %#v", err)
	}

	lc := &logging.Config{}
	oc := &metrics.ObservabilityConfig{}
	ac := &autoscaler.Config{}
	cc := &deployment.Config{}
	want := &corev1.Container{
		// These are effectively constant
		Name:      QueueContainerName,
		Resources: createQueueResources(make(map[string]string), &corev1.Container{}),
		Ports:     append(queueNonServingPorts, queueHTTPPort),
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe", "10"},
				},
			},
			PeriodSeconds:  2,
			TimeoutSeconds: 10,
		},
		// These changed based on the Revision and configs passed in.
		Env:             env(map[string]string{"USER_PORT": strconv.Itoa(userPort)}),
		SecurityContext: queueSecurityContext,
		Args:            []string{"--readiness-probe", string(probeBytes)},
	}

	got := makeQueueContainer(rev, lc, oc, ac, cc)
	sortEnv(got.Env)
	if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
	}
}

func TestProbeGenerationTCPDefaults(t *testing.T) {
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: 1,
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
		},
	}

	expectedProbe := &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(int(v1alpha1.DefaultUserPort)),
			},
		},
		PeriodSeconds:  1,
		TimeoutSeconds: 1,
	}
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to marshall readiness probe %#v", err)
	}

	lc := &logging.Config{}
	oc := &metrics.ObservabilityConfig{}
	ac := &autoscaler.Config{}
	cc := &deployment.Config{}
	want := &corev1.Container{
		// These are effectively constant
		Name:      QueueContainerName,
		Resources: createQueueResources(make(map[string]string), &corev1.Container{}),
		Ports:     append(queueNonServingPorts, queueHTTPPort),
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe", "1"},
				},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 1,
		},
		// These changed based on the Revision and configs passed in.
		Env:             env(map[string]string{}),
		SecurityContext: queueSecurityContext,
		Args:            []string{"--readiness-probe", string(probeBytes)},
	}

	got := makeQueueContainer(rev, lc, oc, ac, cc)
	sortEnv(got.Env)
	if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
	}
}

func TestProbeGenerationTCP(t *testing.T) {
	userPort := 12345
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       ptr.Int64(45),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: containerName,
						Ports: []v1.ContainerPort{{
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
		},
	}

	expectedProbe := &corev1.Probe{
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
	}
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to marshall readiness probe %#v", err)
	}

	lc := &logging.Config{}
	oc := &metrics.ObservabilityConfig{}
	ac := &autoscaler.Config{}
	cc := &deployment.Config{}
	want := &corev1.Container{
		// These are effectively constant
		Name:      QueueContainerName,
		Resources: createQueueResources(make(map[string]string), &corev1.Container{}),
		Ports:     append(queueNonServingPorts, queueHTTPPort),
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe", "15"},
				},
			},
			PeriodSeconds:       2,
			TimeoutSeconds:      15,
			SuccessThreshold:    2,
			FailureThreshold:    7,
			InitialDelaySeconds: 3,
		},
		// These changed based on the Revision and configs passed in.
		Env:             env(map[string]string{"USER_PORT": strconv.Itoa(userPort)}),
		SecurityContext: queueSecurityContext,
		Args:            []string{"--readiness-probe", string(probeBytes)},
	}

	got := makeQueueContainer(rev, lc, oc, ac, cc)
	sortEnv(got.Env)
	if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
	}
}

func TestKProbeGenerationTCP(t *testing.T) {
	userPort := 12345
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
			UID:       "1234",
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: 1,
				TimeoutSeconds:       ptr.Int64(45),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: containerName,
						Ports: []v1.ContainerPort{{
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
		},
	}

	expectedProbe := &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromInt(userPort),
			},
		},
		PeriodSeconds:    0,
		SuccessThreshold: 3,
	}
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to marshall readiness probe %#v", err)
	}

	lc := &logging.Config{}
	oc := &metrics.ObservabilityConfig{}
	ac := &autoscaler.Config{}
	cc := &deployment.Config{}
	want := &corev1.Container{
		// These are effectively constant
		Name:      QueueContainerName,
		Resources: createQueueResources(make(map[string]string), &corev1.Container{}),
		Ports:     append(queueNonServingPorts, queueHTTPPort),
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/ko-app/queue", "-probe", "0"},
				},
			},
			PeriodSeconds:  1,
			TimeoutSeconds: 10,
		},
		// These changed based on the Revision and configs passed in.
		Env:             env(map[string]string{"USER_PORT": strconv.Itoa(userPort)}),
		SecurityContext: queueSecurityContext,
		Args:            []string{"--readiness-probe", string(probeBytes)},
	}

	got := makeQueueContainer(rev, lc, oc, ac, cc)
	sortEnv(got.Env)
	if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
		t.Errorf("makeQueueContainerWithPercentageAnnotation (-want, +got) = %v", diff)
	}
}

var defaultEnv = map[string]string{
	"SERVING_NAMESPACE":               "foo",
	"SERVING_SERVICE":                 "",
	"SERVING_CONFIGURATION":           "",
	"SERVING_REVISION":                "bar",
	"CONTAINER_CONCURRENCY":           "1",
	"REVISION_TIMEOUT_SECONDS":        "45",
	"SERVING_LOGGING_CONFIG":          "",
	"SERVING_LOGGING_LEVEL":           "",
	"SERVING_REQUEST_LOG_TEMPLATE":    "",
	"SERVING_REQUEST_METRICS_BACKEND": "",
	"USER_PORT":                       strconv.Itoa(v1alpha1.DefaultUserPort),
	"SYSTEM_NAMESPACE":                system.Namespace(),
	"METRICS_DOMAIN":                  pkgmetrics.Domain(),
	"QUEUE_SERVING_PORT":              "8012",
	"USER_CONTAINER_NAME":             containerName,
	"ENABLE_VAR_LOG_COLLECTION":       "false",
	"VAR_LOG_VOLUME_NAME":             varLogVolumeName,
	"INTERNAL_VOLUME_PATH":            internalVolumePath,
}

func probeArgs(containerArgs []string, probe *corev1.Probe) []string {
	probeArgs := []string{"--readiness-probe", "null"}
	if probe != nil {
		probeBytes, err := json.Marshal(probe)
		if err != nil {
			return append(containerArgs, probeArgs...)
		}
		return append(containerArgs, []string{"--readiness-probe", string(probeBytes)}...)
	}
	return append(containerArgs, probeArgs...)
}

func env(overrides map[string]string) []corev1.EnvVar {
	values := resources.UnionMaps(defaultEnv, overrides)

	var env []corev1.EnvVar
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
