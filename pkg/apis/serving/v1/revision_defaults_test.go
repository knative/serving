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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
)

var (
	defaultResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	defaultProbe = &corev1.Probe{
		SuccessThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}
	ignoreUnexportedResources = cmpopts.IgnoreUnexported(resource.Quantity{})
)

func TestRevisionDefaulting(t *testing.T) {
	logger := logtesting.TestLogger(t)
	tests := []struct {
		name string
		in   *Revision
		want *Revision
		wc   func(context.Context) context.Context
	}{{
		name: "empty",
		in:   &Revision{},
		want: &Revision{Spec: RevisionSpec{
			TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
			ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
		}},
	}, {
		name: "with context",
		in:   &Revision{Spec: RevisionSpec{PodSpec: corev1.PodSpec{Containers: []corev1.Container{{}}}}},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"revision-timeout-seconds": "123",
				},
			})

			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(123),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
				},
			},
		},
	}, {
		name: "with service links `true`",
		in:   &Revision{Spec: RevisionSpec{PodSpec: corev1.PodSpec{Containers: []corev1.Container{{}}}}},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"enable-service-links": "true",
				},
			})
			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(300),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
					EnableServiceLinks: ptr.Bool(true),
				},
			},
		},
	}, {
		name: "with service links `false`",
		in:   &Revision{Spec: RevisionSpec{PodSpec: corev1.PodSpec{Containers: []corev1.Container{{}}}}},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"enable-service-links": "false",
				},
			})
			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(300),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
					EnableServiceLinks: ptr.Bool(false),
				},
			},
		},
	}, {
		name: "with service set",
		in: &Revision{Spec: RevisionSpec{PodSpec: corev1.PodSpec{
			EnableServiceLinks: ptr.Bool(false),
			Containers:         []corev1.Container{{}},
		}}},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"enable-service-links": "true", // this should be ignored.
				},
			})
			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(300),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
					EnableServiceLinks: ptr.Bool(false),
				},
			},
		},
	}, {
		name: "readonly volumes",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "foo",
						VolumeMounts: []corev1.VolumeMount{{
							Name: "bar",
						}},
					}},
				},
				ContainerConcurrency: ptr.Int64(1),
				TimeoutSeconds:       ptr.Int64(99),
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  config.DefaultUserContainerName,
						Image: "foo",
						VolumeMounts: []corev1.VolumeMount{{
							Name:     "bar",
							ReadOnly: true,
						}},
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
				},
				ContainerConcurrency: ptr.Int64(1),
				TimeoutSeconds:       ptr.Int64(99),
			},
		},
	}, {
		name: "timeout sets to default when 0 is specified",
		in:   &Revision{Spec: RevisionSpec{PodSpec: corev1.PodSpec{Containers: []corev1.Container{{}}}, TimeoutSeconds: ptr.Int64(0)}},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"revision-timeout-seconds": "456",
				},
			})

			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(456),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
				},
			},
		},
	}, {
		name: "no overwrite",
		in: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				TimeoutSeconds:       ptr.Int64(99),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "foo",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								TCPSocket: &corev1.TCPSocketAction{
									Host: "127.0.0.2",
								},
							},
						},
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				TimeoutSeconds:       ptr.Int64(99),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "foo",
						Resources: defaultResources,
						ReadinessProbe: &corev1.Probe{
							SuccessThreshold: 1,
							Handler: corev1.Handler{
								TCPSocket: &corev1.TCPSocketAction{
									Host: "127.0.0.2",
								},
							},
						},
					}},
				},
			},
		},
	}, {
		name: "no overwrite exec",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								Exec: &corev1.ExecAction{
									Command: []string{"echo", "hi"},
								},
							},
						},
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      config.DefaultUserContainerName,
						Resources: defaultResources,
						ReadinessProbe: &corev1.Probe{
							SuccessThreshold: 1,
							Handler: corev1.Handler{
								Exec: &corev1.ExecAction{
									Command: []string{"echo", "hi"},
								},
							},
						},
					}},
				},
			},
		},
	}, {
		name: "partially initialized",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{Containers: []corev1.Container{{}}},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
				},
			},
		},
	}, {
		name: "with resources from context",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{Containers: []corev1.Container{{}}},
			},
		},
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: config.DefaultsConfigName,
				},
				Data: map[string]string{
					"revision-cpu-request":               "100m",
					"revision-memory-request":            "200M",
					"revision-ephemeral-storage-request": "300m",
					"revision-cpu-limit":                 "400M",
					"revision-memory-limit":              "500m",
					"revision-ephemeral-storage-limit":   "600M",
				},
			})

			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: config.DefaultUserContainerName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:              resource.MustParse("100m"),
								corev1.ResourceMemory:           resource.MustParse("200M"),
								corev1.ResourceEphemeralStorage: resource.MustParse("300m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:              resource.MustParse("400M"),
								corev1.ResourceMemory:           resource.MustParse("500m"),
								corev1.ResourceEphemeralStorage: resource.MustParse("600M"),
							},
						},
						ReadinessProbe: defaultProbe,
					}},
				},
			},
		},
	}, {
		name: "multiple containers",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "busybox",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name: "helloworld",
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           "busybox",
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name:      "helloworld",
						Resources: defaultResources,
					}},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got.SetDefaults(ctx)
			if !cmp.Equal(test.want, got, ignoreUnexportedResources) {
				t.Errorf("SetDefaults (-want, +got) = %v",
					cmp.Diff(test.want, got, ignoreUnexportedResources))
			}
		})
	}
}

func TestRevisionDefaultingContainerName(t *testing.T) {
	got := &Revision{
		Spec: RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{}, {}},
			},
		},
	}
	got.SetDefaults(context.Background())
	if got.Spec.Containers[0].Name == "" && got.Spec.Containers[1].Name == "" {
		t.Errorf("Failed to set default values for container name")
	}
}
