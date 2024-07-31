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

	"knative.dev/pkg/apis"
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
		ProbeHandler: corev1.ProbeHandler{
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
		want: &Revision{
			Spec: RevisionSpec{
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
			},
		},
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
					"revision-timeout-seconds": "423",
				},
			})

			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(423),
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
		name: "all revision timeouts set",
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
					"revision-timeout-seconds":                "423",
					"revision-idle-timeout-seconds":           "100",
					"revision-response-start-timeout-seconds": "50",
				},
			})
			return s.ToContext(ctx)
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency:        ptr.Int64(0),
				TimeoutSeconds:              ptr.Int64(423),
				ResponseStartTimeoutSeconds: ptr.Int64(50),
				IdleTimeoutSeconds:          ptr.Int64(100),
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
		name: "with context, in create, expect ESL set",
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
					"revision-timeout-seconds": "323",
				},
			})

			return apis.WithinCreate(s.ToContext(ctx))
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(0),
				TimeoutSeconds:       ptr.Int64(323),
				PodSpec: corev1.PodSpec{
					EnableServiceLinks: ptr.Bool(false),
					Containers: []corev1.Container{{
						Name:           config.DefaultUserContainerName,
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}},
				},
			},
		},
	}, {
		name: "with service spec `true`",
		in: &Revision{Spec: RevisionSpec{
			PodSpec: corev1.PodSpec{
				EnableServiceLinks: ptr.Bool(true),
				Containers:         []corev1.Container{{}},
			},
		}},
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
		name: "with service links CM `true`",
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
			return apis.WithinCreate(s.ToContext(ctx))
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
			return apis.WithinCreate(s.ToContext(ctx))
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
					EnableServiceLinks: ptr.Bool(false),
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
					EnableServiceLinks: ptr.Bool(false),
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
							ProbeHandler: corev1.ProbeHandler{
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
							ProbeHandler: corev1.ProbeHandler{
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
							ProbeHandler: corev1.ProbeHandler{
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
							ProbeHandler: corev1.ProbeHandler{
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
		name: "apply k8s defaults when period seconds has a non zero value",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						ReadinessProbe: &corev1.Probe{
							// FailureThreshold and TimeoutSeconds missing
							PeriodSeconds: 10,
						},
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: config.DefaultUserContainerName,
						ReadinessProbe: &corev1.Probe{
							FailureThreshold: 3, // Added as k8s default
							ProbeHandler:     defaultProbe.ProbeHandler,
							PeriodSeconds:    10,
							SuccessThreshold: 1,
							TimeoutSeconds:   1, // Added as k8s default
						},
						Resources: defaultResources,
					}},
				},
				TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
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
	}, {
		name: "multiple containers with some names empty",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "",
					}, {
						Name: "user-container-0",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name: "user-container-3",
					}, {
						Name: "",
					}, {
						Name: "user-container-5",
					}, {
						Name: "",
					}, {
						Name: "",
					}, {
						Name: "user-container-4",
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
						Name:      "user-container-1",
						Resources: defaultResources,
					}, {
						Name:           "user-container-0",
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name:      "user-container-3",
						Resources: defaultResources,
					}, {
						Name:      "user-container-2",
						Resources: defaultResources,
					}, {
						Name:      "user-container-5",
						Resources: defaultResources,
					}, {
						Name:      "user-container-6",
						Resources: defaultResources,
					}, {
						Name:      "user-container-7",
						Resources: defaultResources,
					}, {
						Name:      "user-container-4",
						Resources: defaultResources,
					}},
				},
			},
		},
	}, {
		name: "no SetDefaults if update revision",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "user-container-1",
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "user-container-1",
					}},
				},
			},
		},
		wc: func(ctx context.Context) context.Context {
			ctx = apis.WithinUpdate(ctx, "fake")
			return ctx
		},
	}, {
		name: "multiple init containers",
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
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
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
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}},
				},
			},
		},
	}, {
		name: "multiple init containers with some names empty",
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
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}, {
						Name: "",
					}, {
						Name: "",
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
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}, {
						Name: "init-container-0",
					}, {
						Name: "init-container-1",
					}},
				},
			},
		},
	}, {
		name: "multiple init and user containers with some names empty",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name: "",
					}},
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}, {
						Name: "",
					}, {
						Name: "",
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
						Name:           "user-container-0",
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name:      "user-container-1",
						Resources: defaultResources,
					}},
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}, {
						Name: "init-container-0",
					}, {
						Name: "init-container-1",
					}},
				},
			},
		},
	}, {
		name: "multiple init and user containers with some conflicting default names",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "init-container-0",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name: "init-container-1",
					}},
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}, {
						Name: "",
					}, {
						Name: "",
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
						Name:           "init-container-0",
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8888,
						}},
					}, {
						Name:      "init-container-1",
						Resources: defaultResources,
					}},
					InitContainers: []corev1.Container{{
						Name: "init1",
					}, {
						Name: "init2",
					}, {
						Name: "init-container-2",
					}, {
						Name: "init-container-3",
					}},
				},
			},
		},
	}, {
		name: "Default security context with feature enabled",
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.DefaultsConfigName}})
			s.OnConfigChanged(
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName},
					Data:       map[string]string{"secure-pod-defaults": "Enabled"},
				},
			)

			return s.ToContext(ctx)
		},
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "user-container",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 80,
						}},
					}, {
						Name:            "sidecar",
						SecurityContext: &corev1.SecurityContext{},
					}, {
						Name: "special-sidecar",
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Add:  []corev1.Capability{"NET_ADMIN"},
								Drop: []corev1.Capability{},
							},
						},
					}},
					InitContainers: []corev1.Container{{
						Name: "special-init",
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(true),
							SeccompProfile: &corev1.SeccompProfile{
								Type:             corev1.SeccompProfileTypeLocalhost,
								LocalhostProfile: ptr.String("special"),
							},
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"NET_ADMIN"},
							},
						},
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "user-container",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 80,
						}},
						ReadinessProbe: defaultProbe,
						Resources:      defaultResources,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(false),
							RunAsNonRoot:             ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
								Add:  []corev1.Capability{"NET_BIND_SERVICE"},
							},
						},
					}, {
						Name:      "sidecar",
						Resources: defaultResources,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(false),
							RunAsNonRoot:             ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
					}, {
						Name:      "special-sidecar",
						Resources: defaultResources,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(true),
							RunAsNonRoot:             ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Add:  []corev1.Capability{"NET_ADMIN"},
								Drop: []corev1.Capability{},
							},
						},
					}},
					InitContainers: []corev1.Container{{
						Name: "special-init",
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(true),
							SeccompProfile: &corev1.SeccompProfile{
								Type:             corev1.SeccompProfileTypeLocalhost,
								LocalhostProfile: ptr.String("special"),
							},
							RunAsNonRoot: ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"NET_ADMIN"},
							},
						},
					}},
				},
			},
		},
	}, {
		name: "uses pod defaults in security context",
		wc: func(ctx context.Context) context.Context {
			s := config.NewStore(logger)
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: autoscalerconfig.ConfigName}})
			s.OnConfigChanged(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: config.DefaultsConfigName}})
			s.OnConfigChanged(
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: config.FeaturesConfigName},
					Data:       map[string]string{"secure-pod-defaults": "Enabled"},
				},
			)

			return s.ToContext(ctx)
		},
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "user-container",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
						}},
					}},
					InitContainers: []corev1.Container{{
						Name:            "init",
						SecurityContext: &corev1.SecurityContext{},
					}},
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeUnconfined,
						},
					},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "user-container",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
						}},
						ReadinessProbe: defaultProbe,
						Resources:      defaultResources,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(false),
							RunAsNonRoot:             ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
					}},
					InitContainers: []corev1.Container{{
						Name: "init",
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.Bool(false),
							RunAsNonRoot:             ptr.Bool(true),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
					}},
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeUnconfined,
						},
					},
				},
			},
		},
	}, {
		name: "multiple containers with default probes",
		in: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				TimeoutSeconds:       ptr.Int64(99),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "foo",
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Host: "127.0.0.2",
								},
							},
						},
					}, {
						Name: "second",
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
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Host: "127.0.0.2",
								},
							},
						},
					}, {
						Name:      "second",
						Resources: defaultResources,
					}},
				},
			},
		},
	}, {
		name: "multiple containers with probes no override",
		in: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
				TimeoutSeconds:       ptr.Int64(99),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "foo",
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Host: "127.0.0.2",
								},
							},
						},
					}, {
						Name: "second",
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
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
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Host: "127.0.0.2",
								},
							},
						},
					}, {
						Name:      "second",
						Resources: defaultResources,
						ReadinessProbe: &corev1.Probe{
							SuccessThreshold: 1,
							ProbeHandler: corev1.ProbeHandler{
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
		name: "multiple containers with exec probes no override",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{"echo", "hi"},
								},
							},
						},
					}, {
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
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
						Name:      config.DefaultUserContainerName + "-0",
						Resources: defaultResources,
						ReadinessProbe: &corev1.Probe{
							SuccessThreshold: 1,
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{"echo", "hi"},
								},
							},
						},
					}, {
						Name:      config.DefaultUserContainerName + "-1",
						Resources: defaultResources,
						ReadinessProbe: &corev1.Probe{
							SuccessThreshold: 1,
							ProbeHandler: corev1.ProbeHandler{
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
		name: "multiple containers apply k8s defaults when period seconds has a non zero value",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
						}},
						ReadinessProbe: &corev1.Probe{
							// FailureThreshold and TimeoutSeconds missing
							PeriodSeconds: 10,
						},
					}, {
						ReadinessProbe: &corev1.Probe{
							// FailureThreshold and TimeoutSeconds missing
							PeriodSeconds: 10,
						},
					}},
				},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: config.DefaultUserContainerName + "-0",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
						}},
						ReadinessProbe: &corev1.Probe{
							FailureThreshold: 3, // Added as k8s default
							ProbeHandler:     defaultProbe.ProbeHandler,
							PeriodSeconds:    10,
							SuccessThreshold: 1,
							TimeoutSeconds:   1, // Added as k8s default
						},
						Resources: defaultResources,
					}, {
						Name: config.DefaultUserContainerName + "-1",
						ReadinessProbe: &corev1.Probe{
							FailureThreshold: 3, // Added as k8s default
							PeriodSeconds:    10,
							SuccessThreshold: 1,
							TimeoutSeconds:   1, // Added as k8s default
						},
						Resources: defaultResources,
					}},
				},
				TimeoutSeconds: ptr.Int64(config.DefaultRevisionTimeoutSeconds),
			},
		},
	}, {
		name: "multiple containers partially initialized",
		in: &Revision{
			Spec: RevisionSpec{
				PodSpec: corev1.PodSpec{Containers: []corev1.Container{{
					Ports: []corev1.ContainerPort{{
						ContainerPort: 8080,
					}},
				}, {}}},
			},
		},
		want: &Revision{
			Spec: RevisionSpec{
				TimeoutSeconds:       ptr.Int64(config.DefaultRevisionTimeoutSeconds),
				ContainerConcurrency: ptr.Int64(config.DefaultContainerConcurrency),
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: config.DefaultUserContainerName + "-0",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
						}},
						Resources:      defaultResources,
						ReadinessProbe: defaultProbe,
					}, {
						Name:           config.DefaultUserContainerName + "-1",
						Resources:      defaultResources,
						ReadinessProbe: nil,
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
				Containers:     []corev1.Container{{}, {}},
				InitContainers: []corev1.Container{{}, {}},
			},
		},
	}
	got.SetDefaults(context.Background())
	if got.Spec.Containers[0].Name == "" && got.Spec.Containers[1].Name == "" {
		t.Errorf("Failed to set default values for container name")
	}
	if got.Spec.InitContainers[0].Name == "" && got.Spec.InitContainers[1].Name == "" {
		t.Errorf("Failed to set default values for init container name")
	}
}
