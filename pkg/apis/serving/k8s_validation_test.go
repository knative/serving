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

package serving

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"

	"knative.dev/serving/pkg/apis/config"
)

type configOption func(*config.Config) *config.Config

type containerValidationTestCase struct {
	name    string
	c       corev1.Container
	want    *apis.FieldError
	volumes map[string]corev1.Volume
	cfgOpts []configOption
}

func withMultiContainerDisabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.MultiContainer = config.Disabled
		return cfg
	}
}

func withPodSpecFieldRefEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecFieldRef = config.Enabled
		return cfg
	}
}

func withPodSpecAffinityEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecAffinity = config.Enabled
		return cfg
	}
}

func withPodSpecTopologySpreadConstraintsEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecTopologySpreadConstraints = config.Enabled
		return cfg
	}
}

func withPodSpecHostAliasesEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecHostAliases = config.Enabled
		return cfg
	}
}

func withPodSpecNodeSelectorEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecNodeSelector = config.Enabled
		return cfg
	}
}

func withPodSpecTolerationsEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecTolerations = config.Enabled
		return cfg
	}
}

func withPodSpecRuntimeClassNameEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecRuntimeClassName = config.Enabled
		return cfg
	}
}

func withPodSpecSecurityContextEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecSecurityContext = config.Enabled
		return cfg
	}
}

func withContainerSpecAddCapabilitiesEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.ContainerSpecAddCapabilities = config.Enabled
		return cfg
	}
}

func withPodSpecVolumesEmptyDirEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecVolumesEmptyDir = config.Enabled
		return cfg
	}
}

func withPodSpecPersistentVolumeClaimEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecPersistentVolumeClaim = config.Enabled
		return cfg
	}
}

func withPodSpecPersistentVolumeWriteEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecPersistentVolumeWrite = config.Enabled
		return cfg
	}
}

func withPodSpecPriorityClassNameEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecPriorityClassName = config.Enabled
		return cfg
	}
}

func withPodSpecSchedulerNameEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecSchedulerName = config.Enabled
		return cfg
	}
}

func withPodSpecProcessNamespaceEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecShareProcessNamespace = config.Enabled
		return cfg
	}
}

func withPodSpecInitContainersEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecInitContainers = config.Enabled
		return cfg
	}
}

func withMultiContainerProbesEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.MultiContainerProbing = config.Enabled
		return cfg
	}
}

func withPodSpecDNSPolicyEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecDNSPolicy = config.Enabled
		return cfg
	}
}

func withPodSpecDNSConfigEnabled() configOption {
	return func(cfg *config.Config) *config.Config {
		cfg.Features.PodSpecDNSConfig = config.Enabled
		return cfg
	}
}

func TestPodSpecValidation(t *testing.T) {
	tests := []struct {
		name     string
		ps       corev1.PodSpec
		cfgOpts  []configOption
		want     *apis.FieldError
		errLevel apis.DiagnosticLevel
	}{{
		name: "valid",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "helloworld",
			}},
		},
		want: nil,
	}, {
		name: "with volume (ok)",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "the-name",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "foo",
					},
				},
			}},
		},
		want: nil,
	}, {
		name: "with automountServiceAccountToken (ok)",
		ps: corev1.PodSpec{
			AutomountServiceAccountToken: ptr.Bool(false),
			Containers: []corev1.Container{{
				Image: "helloworld",
			}},
		},
		want: nil,
	}, {
		name: "with automountServiceAccountToken = true",
		ps: corev1.PodSpec{
			AutomountServiceAccountToken: ptr.Bool(true),
			Containers: []corev1.Container{{
				Image: "helloworld",
			}},
		},
		want: apis.ErrDisallowedFields("automountServiceAccountToken"),
	}, {
		name: "with volume name collision",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "the-name",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "foo",
					},
				},
			}, {
				Name: "the-name",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{},
				},
			}},
		},
		want: (&apis.FieldError{
			Message: `duplicate volume name "the-name"`,
			Paths:   []string{"name"},
		}).ViaFieldIndex("volumes", 1),
	}, {
		name: "with volume mount path collision",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-foo",
					ReadOnly:  true,
				}, {
					MountPath: "/mount/path",
					Name:      "the-bar",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "the-foo",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "foo",
					},
				},
			}, {
				Name: "the-bar",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "bar",
					},
				},
			}},
		},
		want: apis.ErrInvalidValue(`"/mount/path" must be unique`, "mountPath").
			ViaFieldIndex("volumeMounts", 1).ViaFieldIndex("containers", 0),
	}, {
		name: "bad pod spec",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:      "steve",
				Image:     "helloworld",
				Lifecycle: &corev1.Lifecycle{},
			}},
		},
		want: apis.ErrDisallowedFields("containers[0].lifecycle"),
	}, {
		name: "missing all",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{},
		},
		want: apis.ErrMissingField("containers"),
	}, {
		name: "missing container",
		ps: corev1.PodSpec{
			ServiceAccountName: "bob",
			Containers:         []corev1.Container{},
		},
		want: apis.ErrMissingField("containers"),
	}, {
		name: "too many containers",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				Ports: []corev1.ContainerPort{{}},
			}, {
				Image: "helloworld",
			}},
		},
		cfgOpts: []configOption{withMultiContainerDisabled()},
		want:    &apis.FieldError{Message: "multi-container is off, but found 2 containers"},
	}, {
		name: "extra field",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
			TerminationGracePeriodSeconds: ptr.Int64(5),
		},
		want: apis.ErrDisallowedFields("terminationGracePeriodSeconds"),
	}, {
		name: "bad service account name",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
			ServiceAccountName: "foo@bar.baz",
		},
		want: apis.ErrInvalidValue("foo@bar.baz", "serviceAccountName", strings.Join(validation.IsDNS1123Subdomain("foo@bar.baz"), "\n")),
	}, {
		name: "init containers with no mounted volume",
		ps: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image: "busybox",
				Name:  "install-nodejs-debug-support",
			}},
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "debugging-support-files",
					MountPath: "/dbg",
				}},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "debugging-support-files",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled(), withPodSpecInitContainersEnabled()},
		want:    nil,
	}, {
		name: "user container with no mounted volume and init container with mounted volume",
		ps: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image: "busybox",
				Name:  "install-nodejs-debug-support",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "debugging-support-files",
					MountPath: "/dbg",
				}},
			}},
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
			Volumes: []corev1.Volume{
				{
					Name: "debugging-support-files",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled(), withPodSpecInitContainersEnabled()},
		want:    nil,
	}, {
		name: "user and init-containers with multiple volumes",
		ps: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image: "busybox",
				Name:  "install-nodejs-debug-support",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "debugging-support-files",
					MountPath: "/dbg",
				}},
			}},
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "data",
					MountPath: "/data",
				}},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "debugging-support-files",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				}, {
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}},
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled(), withPodSpecInitContainersEnabled()},
		want:    nil,
	}, {
		name: "user and init-containers with multiple volumes and one not mounted",
		ps: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image: "busybox",
				Name:  "install-nodejs-debug-support",
			}},
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "data",
					MountPath: "/data",
				}},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "debugging-support-files",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				}, {
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}},
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled(), withPodSpecInitContainersEnabled()},
		want: &apis.FieldError{
			Message: `volume with name "debugging-support-files" not mounted`,
			Paths:   []string{"volumes[0].name"},
		},
	}, {
		name: "init-container name collision",
		ps: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  "the-name",
				Image: "busybox",
			}},
			Containers: []corev1.Container{{
				Name:  "the-name",
				Image: "busybox",
			}},
		},
		cfgOpts: []configOption{withPodSpecInitContainersEnabled()},
		want: (&apis.FieldError{
			Message: `duplicate container name "the-name"`,
			Paths:   []string{"name"},
		}).ViaFieldIndex("containers", 0),
	}, {
		name: "container name collision",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "the-name",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Name:  "the-name",
				Image: "busybox",
			}},
		},
		want: (&apis.FieldError{
			Message: `duplicate container name "the-name"`,
			Paths:   []string{"name"},
		}).ViaFieldIndex("containers", 1),
	}, {
		name: "PVC not read-only, mount read-only, write disabled by default",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "foo",
					MountPath: "/data",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "foo",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "myclaim",
						ReadOnly:  false,
					},
				}},
			}},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled()},
		want: &apis.FieldError{
			Message: `Persistent volume write support is disabled, but found persistent volume claim myclaim that is not read-only`,
		},
	}, {
		name: "PVC not read-only, mount not read-only, write disabled by default",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "foo",
					MountPath: "/data",
					ReadOnly:  false,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "foo",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "myclaim",
						ReadOnly:  false,
					},
				}},
			}},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled()},
		want: &apis.FieldError{
			Message: `Persistent volume write support is disabled, but found persistent volume claim myclaim that is not read-only`,
		},
	}, {
		name: "PVC read-only, mount not read-only, write disabled by default",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "foo",
					MountPath: "/data",
					ReadOnly:  false,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "foo",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "myclaim",
						ReadOnly:  true,
					},
				}},
			}},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled()},
		want: &apis.FieldError{
			Message: "volume is readOnly but volume mount is not",
			Paths:   []string{"containers[0].volumeMounts[0].readOnly"},
		},
	}, {
		name: "PVC read-only, write disabled by default",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "foo",
					MountPath: "/data",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "foo",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "myclaim",
						ReadOnly:  true,
					},
				}},
			}},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled()},
	}, {
		name: "PVC not read-only, write enabled",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "foo",
					MountPath: "/data",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "foo",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "myclaim",
						ReadOnly:  false,
					},
				}},
			}},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled(), withPodSpecPersistentVolumeWriteEnabled()},
	}, {
		name: "PVC read-only, write enabled",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "foo",
					MountPath: "/data",
					ReadOnly:  false,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "foo",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "myclaim",
						ReadOnly:  false,
					},
				}},
			}},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled(), withPodSpecPersistentVolumeWriteEnabled()},
	}, {
		name: "insecure security context default struct",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
		},
		want: &apis.FieldError{
			Message: "Kubernetes default value is insecure, Knative may default this to secure in a future release",
			Paths: []string{
				"containers[0].securityContext.allowPrivilegeEscalation",
				"containers[0].securityContext.capabilities",
				"containers[0].securityContext.runAsNonRoot",
				"containers[0].securityContext.seccompProfile",
			},
			Level: apis.WarningLevel,
		},
		errLevel: apis.WarningLevel,
	}, {
		name: "insecure security context default values",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{},
				},
			}},
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{},
			},
		},
		want: &apis.FieldError{
			Message: "Kubernetes default value is insecure, Knative may default this to secure in a future release",
			Paths: []string{
				"containers[0].securityContext.allowPrivilegeEscalation",
				"containers[0].securityContext.capabilities.drop",
				"containers[0].securityContext.runAsNonRoot",
				"containers[0].securityContext.seccompProfile.type",
			},
			Level: apis.WarningLevel,
		},
		errLevel: apis.WarningLevel,
	}, {
		// We don't warn about explicitly insecure values, because they were
		// presumably chosen intentionally.
		name: "explicitly dangerous security context values",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.Bool(true),
					RunAsNonRoot:             ptr.Bool(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{},
					},
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeUnconfined,
					},
				},
			}},
		},
		errLevel: apis.WarningLevel,
	}, {
		name: "explicitly dangerous pod security context values",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.Bool(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeUnconfined,
				},
			},
		},
		want: &apis.FieldError{
			Message: "Kubernetes default value is insecure, Knative may default this to secure in a future release",
			Paths: []string{
				"containers[0].securityContext.allowPrivilegeEscalation",
				"containers[0].securityContext.capabilities",
			},
			Level: apis.WarningLevel,
		},
		errLevel: apis.WarningLevel,
	}, {
		name: "misspelled drop all doesn't drop ALL",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.Bool(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"all"},
					},
				},
			}},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.Bool(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
		want:     apis.ErrInvalidValue("all", "containers[0].securityContext.capabilities.drop", "Must be spelled as 'ALL'").At(apis.WarningLevel),
		errLevel: apis.WarningLevel,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			got := ValidatePodSpec(ctx, test.ps)
			got = got.Filter(test.errLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidatePodSpec (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestPodSpecMultiContainerValidation(t *testing.T) {
	tests := []struct {
		name    string
		ps      corev1.PodSpec
		cfgOpts []configOption
		want    *apis.FieldError
	}{{
		name: "flag disabled: more than one container",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Image: "helloworld",
			}},
		},
		cfgOpts: []configOption{withMultiContainerDisabled()},
		want:    &apis.FieldError{Message: "multi-container is off, but found 2 containers"},
	}, {
		name: "flag enabled: more than one container with one container port",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
			}},
		},
		want: nil,
	}, {
		name: "flag enabled: probes are not allowed for non serving containers",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
				LivenessProbe: &corev1.Probe{
					TimeoutSeconds: 1,
				},
				ReadinessProbe: &corev1.Probe{
					TimeoutSeconds: 1,
				},
			}},
		},
		want: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"containers[1].livenessProbe.timeoutSeconds", "containers[1].readinessProbe.timeoutSeconds"},
		},
	}, {
		name: "flag enabled: multiple containers with no port",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
			}, {
				Name:  "container-b",
				Image: "helloworld",
			}},
		},
		want: apis.ErrMissingField("containers[*].ports"),
	}, {
		name: "flag enabled: multiple containers with multiple port",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 9999,
				}},
			}},
		},
		want: &apis.FieldError{
			Message: "more than one container port is set",
			Paths:   []string{"containers[*].ports"},
			Details: "Only a single port is allowed across all containers",
		},
	}, {
		name: "flag enabled: multiple containers with multiple ports for each container",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}, {
					ContainerPort: 9999,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 80,
				}},
			}},
		},
		want: &apis.FieldError{
			Message: "more than one container port is set",
			Paths:   []string{"containers[*].ports"},
			Details: "Only a single port is allowed across all containers",
		},
	}, {
		name: "flag enabled: multiple containers with multiple port for a single container",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}, {
					ContainerPort: 9999,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
			}},
		},
		want: &apis.FieldError{
			Message: "more than one container port is set",
			Paths:   []string{"containers[*].ports"},
			Details: "Only a single port is allowed across all containers",
		},
	}, {
		name: "flag enabled: multiple containers with readinessProbe targeting different container's port",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "work",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 9999,
				}},
			}, {
				Name:  "container-b",
				Image: "health",
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Port: intstr.FromInt(9999), // This is the other container's port
						},
					},
				},
			}},
		},
		want: apis.ErrDisallowedFields("containers[1].livenessProbe"),
	}, {
		name: "flag enabled: multiple containers with illegal env variable defined for side car",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: "Foo",
				}, {
					Name:  "K_SERVICE",
					Value: "Foo",
				}},
			}},
		},
		want: &apis.FieldError{
			Message: `"K_SERVICE" is a reserved environment variable`,
			Paths:   []string{"containers[1].env[1].name"},
		},
	}, {
		name: "flag enabled: multiple containers with PORT defined for side car",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			}, {
				Name:  "container-b",
				Image: "helloworld",
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: "Foo",
				}},
			}},
		},
		want: nil,
	}, {
		name: "Volume mounts ok with single container",
		ps: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo",
						},
					}},
			},
			Containers: []corev1.Container{{
				Image: "busybox",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			}},
		},
		cfgOpts: []configOption{withPodSpecFieldRefEnabled()},
	}, {
		name: "Volume not mounted when having a single container",
		ps: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo",
						},
					}},
			},
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
		},
		cfgOpts: []configOption{withPodSpecFieldRefEnabled()},
		want: &apis.FieldError{
			Message: `volume with name "the-name" not mounted`,
			Paths:   []string{"volumes[0].name"}},
	}, {
		name: "Volume mounts ok when having multiple containers",
		ps: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "the-name1",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo1",
						},
					},
				},
				{Name: "the-name2",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo2",
						},
					}},
			},
			Containers: []corev1.Container{
				{
					Name:  "container-a",
					Image: "busybox",
					Ports: []corev1.ContainerPort{{ContainerPort: 8888}},
					VolumeMounts: []corev1.VolumeMount{{
						MountPath: "/mount/path",
						Name:      "the-name1",
						ReadOnly:  true,
					}},
				},
				{
					Name:  "container-b",
					Image: "busybox",
					VolumeMounts: []corev1.VolumeMount{{
						MountPath: "/mount/path",
						Name:      "the-name2",
						ReadOnly:  true,
					}},
				},
			},
		},
	}, {
		name: "Volume not mounted when having multiple containers",
		ps: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "the-name1",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo1",
						},
					},
				},
				{Name: "the-name2",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "foo2",
						},
					}},
			},
			Containers: []corev1.Container{
				{
					Name:  "container-a",
					Image: "busybox",
					Ports: []corev1.ContainerPort{{ContainerPort: 8888}},
					VolumeMounts: []corev1.VolumeMount{{
						MountPath: "/mount/path",
						Name:      "the-name1",
						ReadOnly:  true,
					}},
				},
				{
					Name:  "container-b",
					Image: "busybox"},
			},
		},
		want: &apis.FieldError{
			Message: `volume with name "the-name2" not mounted`,
			Paths:   []string{"volumes[1].name"},
		},
	}, {
		name: "multiple containers and init-containers with multiple volumes",
		ps: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image: "busybox",
				Name:  "install-nodejs-debug-support",
			}},
			Containers: []corev1.Container{{
				Name:  "container-a",
				Image: "busybox1",
				Ports: []corev1.ContainerPort{{ContainerPort: 8888}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "data",
					MountPath: "/data",
				}},
			}, {
				Name:  "container-b",
				Image: "busybox2",
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "debugging-support-files",
					MountPath: "/dbg",
				}},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "debugging-support-files",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				}, {
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}},
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled(), withPodSpecInitContainersEnabled()},
		want:    nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			got := ValidatePodSpec(ctx, test.ps)
			got = got.Filter(apis.ErrorLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidatePodSpec (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestPodSpecFeatureValidation(t *testing.T) {
	runtimeClassName := "test"
	shareProcessNamespace := true

	featureData := []struct {
		name        string
		featureSpec corev1.PodSpec
		cfgOpts     []configOption
		err         *apis.FieldError
		errLevel    apis.DiagnosticLevel
	}{{
		name: "Affinity",
		featureSpec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchExpressions: []corev1.NodeSelectorRequirement{{
								Key:      "failure-domain.beta.kubernetes.io/zone",
								Operator: "In",
								Values:   []string{"us-east1-b"},
							}},
						}},
					},
				},
			},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"affinity"},
		},
		cfgOpts: []configOption{withPodSpecAffinityEnabled()},
	}, {
		name: "TopologySpreadConstraints",
		featureSpec: corev1.PodSpec{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
				MaxSkew:           1,
				TopologyKey:       "topology.kubernetes.io/zone",
				WhenUnsatisfiable: "DoNotSchedule",
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      "key",
						Operator: "In",
						Values:   []string{"value"},
					}},
				},
			}},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"topologySpreadConstraints"},
		},
		cfgOpts: []configOption{withPodSpecTopologySpreadConstraintsEnabled()},
	}, {
		name: "DNSPolicy",
		featureSpec: corev1.PodSpec{
			DNSPolicy: corev1.DNSDefault,
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"dnsPolicy"},
		},
		cfgOpts: []configOption{withPodSpecDNSPolicyEnabled()},
	}, {
		name: "DNSConfig",
		featureSpec: corev1.PodSpec{
			DNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"1.2.3.4"},
			},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"dnsConfig"},
		},
		cfgOpts: []configOption{withPodSpecDNSConfigEnabled()},
	}, {
		name: "HostAliases",
		featureSpec: corev1.PodSpec{
			HostAliases: []corev1.HostAlias{{
				IP:        "127.0.0.1",
				Hostnames: []string{"foo.remote", "bar.remote"},
			}},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"hostAliases"},
		},
		cfgOpts: []configOption{withPodSpecHostAliasesEnabled()},
	}, {
		name: "NodeSelector",
		featureSpec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/arch": "amd64",
			},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"nodeSelector"},
		},
		cfgOpts: []configOption{withPodSpecNodeSelectorEnabled()},
	}, {
		name: "Tolerations",
		featureSpec: corev1.PodSpec{
			Tolerations: []corev1.Toleration{{
				Key:      "key",
				Operator: "Equal",
				Value:    "value",
				Effect:   "NoSchedule",
			}},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"tolerations"},
		},
		cfgOpts: []configOption{withPodSpecTolerationsEnabled()},
	}, {
		name: "RuntimeClassName",
		featureSpec: corev1.PodSpec{
			RuntimeClassName: &runtimeClassName,
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"runtimeClassName"},
		},
		cfgOpts: []configOption{withPodSpecRuntimeClassNameEnabled()},
	}, {
		name: "PodSpecSecurityContext",
		featureSpec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"securityContext"},
		},
		cfgOpts: []configOption{withPodSpecSecurityContextEnabled()},
	}, {
		name: "PriorityClassName",
		featureSpec: corev1.PodSpec{
			PriorityClassName: "foo",
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"priorityClassName"},
		},
		cfgOpts: []configOption{withPodSpecPriorityClassNameEnabled()},
	}, {
		name: "SchedulerName",
		featureSpec: corev1.PodSpec{
			SchedulerName: "foo",
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"schedulerName"},
		},
		cfgOpts: []configOption{withPodSpecSchedulerNameEnabled()},
	}, {
		name: "ShareProcessNamespace",
		featureSpec: corev1.PodSpec{
			ShareProcessNamespace: &shareProcessNamespace,
		},
		err: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"shareProcessNamespace"},
		},
		cfgOpts: []configOption{withPodSpecProcessNamespaceEnabled()},
	}}

	featureTests := []struct {
		nameTemplate       string
		enableFeature      bool
		includeFeatureSpec bool
		wantError          bool
	}{{
		nameTemplate:       "flag disabled: %s not present",
		enableFeature:      false,
		includeFeatureSpec: false,
		wantError:          false,
	}, {
		nameTemplate:       "flag disabled: %s present",
		enableFeature:      false,
		includeFeatureSpec: true,
		wantError:          true,
	}, {
		nameTemplate:       "flag enabled: %s not present",
		enableFeature:      true,
		includeFeatureSpec: false,
		wantError:          false,
	}, {
		nameTemplate:       "flag enabled: %s present",
		enableFeature:      true,
		includeFeatureSpec: true,
		wantError:          false,
	}}

	for _, featureData := range featureData {
		for _, test := range featureTests {
			t.Run(fmt.Sprintf(test.nameTemplate, featureData.name), func(t *testing.T) {
				ctx := context.Background()
				obj := corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				}
				want := &apis.FieldError{}
				if test.wantError {
					want = featureData.err
				}
				if test.enableFeature {
					cfg := config.FromContextOrDefaults(ctx)
					for _, opt := range featureData.cfgOpts {
						cfg = opt(cfg)
					}
					ctx = config.ToContext(ctx, cfg)
				}
				if test.includeFeatureSpec {
					obj = featureData.featureSpec
					obj.Containers = []corev1.Container{{
						Image: "busybox",
					}}
				}
				got := ValidatePodSpec(ctx, obj)
				got = got.Filter(featureData.errLevel)
				if diff := cmp.Diff(want.Error(), got.Error()); diff != "" {
					t.Errorf("ValidatePodSpec (-want, +got): \n%s", diff)
				}
			})
		}
	}
}

func TestPodSpecFieldRefValidation(t *testing.T) {
	tests := []struct {
		name     string
		ps       corev1.PodSpec
		cfgOpts  []configOption
		want     *apis.FieldError
		errLevel apis.DiagnosticLevel
	}{{
		name: "flag disabled: fieldRef not present",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
		},
	}, {
		name: "flag disabled: fieldRef present",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				Env: []corev1.EnvVar{{
					Name: "NODE_IP",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "status.hostIP",
						},
					},
				}},
			}},
		},
		want: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"containers[0].env[0].valueFrom.fieldRef"},
		},
	}, {
		name: "flag disabled: resourceFieldRef present",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				Env: []corev1.EnvVar{{
					Name: "NODE_IP",
					ValueFrom: &corev1.EnvVarSource{
						ResourceFieldRef: &corev1.ResourceFieldSelector{
							ContainerName: "Server",
							Resource:      "request.cpu",
						},
					},
				}},
			}},
		},
		want: &apis.FieldError{
			Message: "must not set the field(s)",
			Paths:   []string{"containers[0].env[0].valueFrom.resourceFieldRef"},
		},
	}, {
		name: "flag enabled: fieldRef not present",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
		},
		cfgOpts: []configOption{withPodSpecFieldRefEnabled()},
	}, {
		name: "flag enabled: fieldRef present",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				Env: []corev1.EnvVar{{
					Name: "NODE_IP",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "status.hostIP",
						},
					},
				}},
			}},
		},
		cfgOpts: []configOption{withPodSpecFieldRefEnabled()},
	}, {
		name: "flag enabled: resourceFieldRef present",
		ps: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
				Env: []corev1.EnvVar{{
					Name: "NODE_IP",
					ValueFrom: &corev1.EnvVarSource{
						ResourceFieldRef: &corev1.ResourceFieldSelector{
							ContainerName: "Server",
							Resource:      "request.cpu",
						},
					},
				}},
			}},
		},
		cfgOpts: []configOption{withPodSpecFieldRefEnabled()},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			got := ValidatePodSpec(ctx, test.ps)
			got = got.Filter(test.errLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidatePodSpec (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestUserContainerValidation(t *testing.T) {
	tests := []containerValidationTestCase{
		{
			name: "has a lifecycle",
			c: corev1.Container{
				Name:      "foo",
				Image:     "foo",
				Lifecycle: &corev1.Lifecycle{},
			},
			want:    apis.ErrDisallowedFields("lifecycle"),
			cfgOpts: []configOption{withPodSpecInitContainersEnabled()},
		}, {
			name: "has lifecycle",
			c: corev1.Container{
				Image:     "foo",
				Lifecycle: &corev1.Lifecycle{},
			},
			want: apis.ErrDisallowedFields("lifecycle"),
		},
		{
			name: "has valid unnamed user port",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8181,
				}},
			},
			want: nil,
		}, {
			name: "has valid user port http1",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					Name: "http1",
				}},
			},
			want: nil,
		}, {
			name: "has valid user port h2c",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					Name: "h2c",
				}},
			},
			want: nil,
		}, {
			name: "has more than one ports with valid names",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					Name: "h2c",
				}, {
					Name: "http1",
				}},
			},
			want: &apis.FieldError{
				Message: "more than one container port is set",
				Paths:   []string{"ports"},
				Details: "Only a single port is allowed across all containers",
			},
		}, {
			name: "has an empty port set",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{}},
			},
			want: nil,
		}, {
			name: "has more than one unnamed port",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8080,
				}, {
					ContainerPort: 8181,
				}},
			},
			want: &apis.FieldError{
				Message: "more than one container port is set",
				Paths:   []string{"ports"},
				Details: "Only a single port is allowed across all containers",
			},
		}, {
			name: "has tcp protocol",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					Protocol: corev1.ProtocolTCP,
				}},
			},
			want: nil,
		}, {
			name: "has invalid protocol",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					Protocol: "tdp",
				}},
			},
			want: apis.ErrInvalidValue("tdp", "ports.protocol"),
		}, {
			name: "has host port",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					HostPort: 80,
				}},
			},
			want: apis.ErrDisallowedFields("ports.hostPort"),
		}, {
			name: "has invalid port name",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					Name: "foobar",
				}},
			},
			want: &apis.FieldError{
				Message: fmt.Sprintf("Port name %v is not allowed", "foobar"),
				Paths:   []string{"ports"},
				Details: "Name must be empty, or one of: 'h2c', 'http1'",
			},
		}, {
			name: "valid with probes (no port)",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{},
					},
				},
			},
			want: nil,
		}, {
			name: "valid with exec probes ",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					InitialDelaySeconds: 0,
					PeriodSeconds:       1,
					TimeoutSeconds:      1,
					SuccessThreshold:    1,
					FailureThreshold:    3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{},
					},
				},
			},
			want: nil,
		}, {
			name: "invalid with no handler",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{},
				},
			},
			want: apis.ErrMissingOneOf("livenessProbe.httpGet", "livenessProbe.tcpSocket", "livenessProbe.exec", "livenessProbe.grpc"),
		}, {
			name: "invalid with multiple handlers",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
						Exec:      &corev1.ExecAction{},
						TCPSocket: &corev1.TCPSocketAction{},
					},
				},
			},
			want: apis.ErrMultipleOneOf("readinessProbe.exec", "readinessProbe.tcpSocket", "readinessProbe.httpGet"),
		}, {
			name: "valid liveness http probe with a different container port",
			c: corev1.Container{
				Image: "foo",
				LivenessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			want: nil,
		}, {
			name: "valid liveness tcp probe with a different container port",
			c: corev1.Container{
				Image: "foo",
				LivenessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			want: nil,
		}, {
			name: "valid readiness http probe with a different container port",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			want: nil,
		}, {
			name: "valid readiness tcp probe with a different container port",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			want: nil,
		}, {
			name: "valid readiness http probe with port",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					SuccessThreshold: 1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Port: intstr.FromString("http"), // http is the default
						},
					},
				},
			},
			want: nil,
		}, {
			name: "invalid readiness probe (has failureThreshold while using special probe)",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    0,
					FailureThreshold: 2,
					SuccessThreshold: 1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
			},
			want: &apis.FieldError{
				Message: "failureThreshold is disallowed when periodSeconds is zero",
				Paths:   []string{"readinessProbe.failureThreshold"},
			},
		}, {
			name: "invalid readiness probe (has timeoutSeconds while using special probe)",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    0,
					TimeoutSeconds:   2,
					SuccessThreshold: 1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
			},
			want: &apis.FieldError{
				Message: "timeoutSeconds is disallowed when periodSeconds is zero",
				Paths:   []string{"readinessProbe.timeoutSeconds"},
			},
		}, {
			name: "out of bounds probe values",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:       -1,
					TimeoutSeconds:      0,
					SuccessThreshold:    0,
					FailureThreshold:    0,
					InitialDelaySeconds: -1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{},
					},
				},
			},
			want: apis.ErrOutOfBoundsValue(-1, 0, math.MaxInt32, "readinessProbe.periodSeconds").Also(
				apis.ErrOutOfBoundsValue(0, 1, math.MaxInt32, "readinessProbe.timeoutSeconds")).Also(
				apis.ErrOutOfBoundsValue(0, 1, math.MaxInt32, "readinessProbe.successThreshold")).Also(
				apis.ErrOutOfBoundsValue(0, 1, math.MaxInt32, "readinessProbe.failureThreshold")).Also(
				apis.ErrOutOfBoundsValue(-1, 0, math.MaxInt32, "readinessProbe.initialDelaySeconds")),
		}, {
			name: "reserved env var name for serving container",
			c: corev1.Container{
				Image: "foo",
				Env: []corev1.EnvVar{{
					Name:  "PORT",
					Value: "Foo",
				}},
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8888,
				}},
			},
			want: &apis.FieldError{
				Message: `"PORT" is a reserved environment variable`,
				Paths:   []string{"env[0].name"},
			},
		}, {
			name: "invalid liveness tcp probe (has port)",
			c: corev1.Container{
				Image: "foo",
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromString("imap"),
						},
					},
				},
			},
			want: apis.ErrInvalidValue("imap", "livenessProbe.tcpSocket.port", "Probe port must match container port"),
		}, {
			name: "valid liveness tcp probe with correct port",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 8888,
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(8888),
						},
					},
				},
			},
		}, {
			name: "disallowed container fields",
			c: corev1.Container{
				Image:     "foo",
				Name:      "fail",
				Stdin:     true,
				StdinOnce: true,
				TTY:       true,
				Lifecycle: &corev1.Lifecycle{},
				VolumeDevices: []corev1.VolumeDevice{{
					Name:       "disallowed",
					DevicePath: "/",
				}},
			},
			want: apis.ErrDisallowedFields("lifecycle").Also(
				apis.ErrDisallowedFields("stdin")).Also(
				apis.ErrDisallowedFields("stdinOnce")).Also(
				apis.ErrDisallowedFields("tty")).Also(
				apis.ErrDisallowedFields("volumeDevices")),
		}, {
			name: "has numerous problems",
			c: corev1.Container{
				Lifecycle: &corev1.Lifecycle{},
			},
			want: apis.ErrDisallowedFields("lifecycle").Also(
				apis.ErrMissingField("image")),
		}, {
			name: "valid grpc probe",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						GRPC: &corev1.GRPCAction{
							Port: 46,
						},
					},
				},
			},
		}, {
			name: "valid grpc probe with service",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						GRPC: &corev1.GRPCAction{
							Port:    46,
							Service: ptr.String("foo"),
						},
					},
				},
			},
		},
	}
	tests = append(tests, getCommonContainerValidationTestCases()...)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			port, err := validateContainersPorts([]corev1.Container{test.c})

			got := err.Also(ValidateUserContainer(ctx, test.c, test.volumes, port))
			got = got.Filter(apis.ErrorLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidateUserContainer (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestSidecarContainerValidation(t *testing.T) {
	tests := []containerValidationTestCase{
		{
			name: "probes not allowed",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{},
					},
				},
			},
			want: apis.ErrDisallowedFields("livenessProbe", "readinessProbe", "readinessProbe.failureThreshold", "readinessProbe.periodSeconds", "readinessProbe.successThreshold", "readinessProbe.timeoutSeconds"),
		},
		{
			name: "invalid probes (no port defined)",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    apis.ErrInvalidValue(0, "livenessProbe.tcpSocket.port, readinessProbe.httpGet.port", "Probe port must be specified"),
		}, {
			name: "valid with exec probes",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					InitialDelaySeconds: 0,
					PeriodSeconds:       1,
					TimeoutSeconds:      1,
					SuccessThreshold:    1,
					FailureThreshold:    3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt32(5000),
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "invalid with no handler",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt32(5000),
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    apis.ErrMissingOneOf("livenessProbe.httpGet", "livenessProbe.tcpSocket", "livenessProbe.exec", "livenessProbe.grpc"),
		}, {
			name: "invalid with multiple handlers",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt32(5000),
						},
						Exec: &corev1.ExecAction{},
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt32(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    apis.ErrMultipleOneOf("readinessProbe.exec", "readinessProbe.tcpSocket", "readinessProbe.httpGet"),
		}, {
			name: "valid liveness http probe",
			c: corev1.Container{
				Image: "foo",
				LivenessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "valid liveness tcp probe",
			c: corev1.Container{
				Image: "foo",
				LivenessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "valid readiness http probe",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "valid readiness tcp probe",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "valid readiness http probe with named port",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					SuccessThreshold: 1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Port: intstr.FromString("http"), // http is the default
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "invalid readiness probe (has failureThreshold while using special probe)",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    0,
					FailureThreshold: 2,
					SuccessThreshold: 1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want: &apis.FieldError{
				Message: "failureThreshold is disallowed when periodSeconds is zero",
				Paths:   []string{"readinessProbe.failureThreshold"},
			},
		}, {
			name: "invalid readiness probe (has timeoutSeconds while using special probe)",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    0,
					TimeoutSeconds:   2,
					SuccessThreshold: 1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want: &apis.FieldError{
				Message: "timeoutSeconds is disallowed when periodSeconds is zero",
				Paths:   []string{"readinessProbe.timeoutSeconds"},
			},
		}, {
			name: "out of bounds probe values",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:       -1,
					TimeoutSeconds:      0,
					SuccessThreshold:    0,
					FailureThreshold:    0,
					InitialDelaySeconds: -1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Port: intstr.FromInt(5000),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want: apis.ErrOutOfBoundsValue(-1, 0, math.MaxInt32, "readinessProbe.periodSeconds").Also(
				apis.ErrOutOfBoundsValue(0, 1, math.MaxInt32, "readinessProbe.timeoutSeconds")).Also(
				apis.ErrOutOfBoundsValue(0, 1, math.MaxInt32, "readinessProbe.successThreshold")).Also(
				apis.ErrOutOfBoundsValue(0, 1, math.MaxInt32, "readinessProbe.failureThreshold")).Also(
				apis.ErrOutOfBoundsValue(-1, 0, math.MaxInt32, "readinessProbe.initialDelaySeconds")),
		}, {
			name: "valid grpc probe",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						GRPC: &corev1.GRPCAction{
							Port: 46,
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		}, {
			name: "valid grpc probe with service",
			c: corev1.Container{
				Image: "foo",
				ReadinessProbe: &corev1.Probe{
					PeriodSeconds:    1,
					TimeoutSeconds:   1,
					SuccessThreshold: 1,
					FailureThreshold: 3,
					ProbeHandler: corev1.ProbeHandler{
						GRPC: &corev1.GRPCAction{
							Port:    46,
							Service: ptr.String("foo"),
						},
					},
				},
			},
			cfgOpts: []configOption{withMultiContainerProbesEnabled()},
			want:    nil,
		},
	}
	tests = append(tests, getCommonContainerValidationTestCases()...)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			err := validateSidecarContainer(ctx, test.c, test.volumes)
			err = err.Filter(apis.ErrorLevel)
			if diff := cmp.Diff(test.want.Error(), err.Error()); diff != "" {
				t.Errorf("validateSidecarContainer (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestInitContainerValidation(t *testing.T) {
	tests := []containerValidationTestCase{
		{
			name: "has a lifecycle",
			c: corev1.Container{
				Name:      "foo",
				Image:     "foo",
				Lifecycle: &corev1.Lifecycle{},
			},
			want: apis.ErrDisallowedFields("lifecycle").Also(&apis.FieldError{
				Message: "field not allowed in an init container",
				Paths:   []string{"lifecycle"},
			}),
			cfgOpts: []configOption{withPodSpecInitContainersEnabled()},
		}, {
			name: "has lifecycle",
			c: corev1.Container{
				Image:     "foo",
				Lifecycle: &corev1.Lifecycle{},
			},
			want: apis.ErrDisallowedFields("lifecycle").Also(&apis.FieldError{
				Message: "field not allowed in an init container",
				Paths:   []string{"lifecycle"},
			}),
		}, {
			name: "invalid liveness tcp probe (has port)",
			c: corev1.Container{
				Image: "foo",
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromString("http"),
						},
					},
				},
			},
			want: &apis.FieldError{
				Message: "field not allowed in an init container",
				Paths:   []string{"livenessProbe"}},
		}, {
			name: "disallowed container fields",
			c: corev1.Container{
				Image:     "foo",
				Name:      "fail",
				Stdin:     true,
				StdinOnce: true,
				TTY:       true,
				Lifecycle: &corev1.Lifecycle{},
				VolumeDevices: []corev1.VolumeDevice{{
					Name:       "disallowed",
					DevicePath: "/",
				}},
			},
			want: apis.ErrDisallowedFields("lifecycle").Also(
				&apis.FieldError{
					Message: "field not allowed in an init container",
					Paths:   []string{"lifecycle"},
				}).Also(
				apis.ErrDisallowedFields("stdin")).Also(
				apis.ErrDisallowedFields("stdinOnce")).Also(
				apis.ErrDisallowedFields("tty")).Also(
				apis.ErrDisallowedFields("volumeDevices")),
		}, {
			name: "has numerous problems",
			c: corev1.Container{
				Lifecycle: &corev1.Lifecycle{},
			},
			want: apis.ErrDisallowedFields("lifecycle").Also(
				&apis.FieldError{
					Message: "field not allowed in an init container",
					Paths:   []string{"lifecycle"},
				}).Also(apis.ErrMissingField("image")),
		},
	}
	tests = append(tests, getCommonContainerValidationTestCases()...)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			got := validateInitContainer(ctx, test.c, test.volumes)
			got = got.Filter(apis.ErrorLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidateInitContainer (-want, +got): \n%s", diff)
			}
		})
	}
}

func getCommonContainerValidationTestCases() []containerValidationTestCase {
	bidir := corev1.MountPropagationBidirectional
	return []containerValidationTestCase{
		{
			name:    "empty container",
			c:       corev1.Container{},
			want:    apis.ErrMissingField(apis.CurrentField),
			cfgOpts: []configOption{withPodSpecInitContainersEnabled()},
		}, {
			name: "valid container",
			c: corev1.Container{
				Image: "foo",
			},
			want: nil,
		}, {
			name: "invalid container image",
			c: corev1.Container{
				Image: "foo:bar:baz",
			},
			want: &apis.FieldError{
				Message: "Failed to parse image reference",
				Paths:   []string{"image"},
				Details: `image: "foo:bar:baz", error: could not parse reference: foo:bar:baz`,
			},
		}, {
			name: "has resources",
			c: corev1.Container{
				Image: "foo",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("250M"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("25m"),
					},
				},
			},
			want: nil,
		}, {
			name: "has no container ports set",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{},
			},
			want: nil,
		}, {
			name: "has container port value too large",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 65536,
				}},
			},
			want: apis.ErrOutOfBoundsValue(65536, 0, 65535, "ports.containerPort"),
		}, {
			name: "has host ip",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					HostIP: "127.0.0.1",
				}},
			},
			want: apis.ErrDisallowedFields("ports.hostIP"),
		}, {
			name: "port conflicts with profiling port",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8008,
				}},
			},
			want: apis.ErrInvalidValue("8008 is a reserved port", "ports.containerPort",
				"8008 is a reserved port, please use a different value"),
		}, {
			name: "port conflicts with queue proxy",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8013,
				}},
			},
			want: apis.ErrInvalidValue("8013 is a reserved port", "ports.containerPort",
				"8013 is a reserved port, please use a different value"),
		}, {
			name: "port conflicts with queue proxy",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8012,
				}},
			},
			want: apis.ErrInvalidValue("8012 is a reserved port", "ports.containerPort",
				"8012 is a reserved port, please use a different value"),
		}, {
			name: "port conflicts with queue proxy metrics",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 9090,
				}},
			},
			want: apis.ErrInvalidValue("9090 is a reserved port", "ports.containerPort",
				"9090 is a reserved port, please use a different value"),
		}, {
			name: "port conflicts with user queue proxy metrics for user",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 9091,
				}},
			},
			want: apis.ErrInvalidValue("9091 is a reserved port", "ports.containerPort",
				"9091 is a reserved port, please use a different value"),
		}, {
			name: "port conflicts with queue proxy admin",
			c: corev1.Container{
				Image: "foo",
				Ports: []corev1.ContainerPort{{
					ContainerPort: 8022,
				}},
			},
			want: apis.ErrInvalidValue("8022 is a reserved port", "ports.containerPort",
				"8022 is a reserved port, please use a different value"),
		}, {
			name: "has unknown volumeMounts",
			c: corev1.Container{
				Image: "foo",
				VolumeMounts: []corev1.VolumeMount{{
					Name:             "the-name",
					SubPath:          "oops",
					MountPropagation: &bidir,
				}},
			},
			want: (&apis.FieldError{
				Message: "volumeMount has no matching volume",
				Paths:   []string{"name"},
			}).ViaFieldIndex("volumeMounts", 0).Also(
				(&apis.FieldError{
					Message: "volume mount should be readOnly for this type of volume",
					Paths:   []string{"readOnly"},
				}).ViaFieldIndex("volumeMounts", 0)).Also(
				apis.ErrMissingField("mountPath").ViaFieldIndex("volumeMounts", 0)).Also(
				apis.ErrDisallowedFields("mountPropagation").ViaFieldIndex("volumeMounts", 0)),
		}, {
			name: "has known volumeMounts",
			c: corev1.Container{
				Image: "foo",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			volumes: map[string]corev1.Volume{
				"the-name": {
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-cm",
							},
						},
					},
				},
			},
		}, {
			name: "has known volumeMounts, but at reserved path",
			c: corev1.Container{
				Image: "foo",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "//dev//",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			volumes: map[string]corev1.Volume{
				"the-name": {
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-cm",
							},
						},
					},
				},
			},
			want: (&apis.FieldError{
				Message: `mountPath "/dev" is a reserved path`,
				Paths:   []string{"mountPath"},
			}).ViaFieldIndex("volumeMounts", 0),
		}, {
			name: "has known volumeMounts, bad mountPath",
			c: corev1.Container{
				Image: "foo",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "not/absolute",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			volumes: map[string]corev1.Volume{
				"the-name": {
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-cm",
							},
						},
					},
				},
			},
			want: apis.ErrInvalidValue("not/absolute", "volumeMounts[0].mountPath"),
		}, {
			name: "Empty dir has rw access",
			c: corev1.Container{
				Image: "foo",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
				}},
			},
			volumes: map[string]corev1.Volume{
				"the-name": {
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium: "Memory",
						},
					},
				},
			},
		}, {
			name: "has known volumeMount twice",
			c: corev1.Container{
				Image: "foo",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}, {
					MountPath: "/another/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			volumes: map[string]corev1.Volume{
				"the-name": {
					Name: "the-name",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-cm",
							},
						},
					},
				},
			},
		}, {
			name: "not allowed to add a security context capability",
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"all"},
					},
				},
			},
			want: apis.ErrDisallowedFields("securityContext.capabilities.add"),
		}, {
			name: "allowed to add a security context capability when gate is enabled",
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"all"},
					},
				},
			},
			cfgOpts: []configOption{withContainerSpecAddCapabilitiesEnabled()},
			want:    nil,
		}, {
			name: "disallowed security context field",
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					Privileged: ptr.Bool(true),
				},
			},
			want: apis.ErrDisallowedFields("securityContext.privileged"),
		}, {
			name: "too large uid",
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.Int64(math.MaxInt32 + 1),
				},
			},
			want: apis.ErrOutOfBoundsValue(int64(math.MaxInt32+1), 0, math.MaxInt32, "securityContext.runAsUser"),
		}, {
			name: "negative uid",
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.Int64(-10),
				},
			},
			want: apis.ErrOutOfBoundsValue(-10, 0, math.MaxInt32, "securityContext.runAsUser"),
		}, {
			name:    "too large gid - feature enabled",
			cfgOpts: []configOption{withPodSpecSecurityContextEnabled()},
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					RunAsGroup: ptr.Int64(math.MaxInt32 + 1),
				},
			},
			want: apis.ErrOutOfBoundsValue(int64(math.MaxInt32+1), 0, math.MaxInt32, "securityContext.runAsGroup"),
		}, {
			name:    "negative gid - feature enabled",
			cfgOpts: []configOption{withPodSpecSecurityContextEnabled()},
			c: corev1.Container{
				Image: "foo",
				SecurityContext: &corev1.SecurityContext{
					RunAsGroup: ptr.Int64(-10),
				},
			},
			want: apis.ErrOutOfBoundsValue(-10, 0, math.MaxInt32, "securityContext.runAsGroup"),
		}, {
			name: "envFrom - None of",
			c: corev1.Container{
				Image:   "foo",
				EnvFrom: []corev1.EnvFromSource{{}},
			},
			want: apis.ErrMissingOneOf("envFrom.configMapRef", "envFrom.secretRef"),
		}, {
			name: "envFrom - Multiple",
			c: corev1.Container{
				Image: "foo",
				EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ConfigMapName",
						},
					},
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "SecretName",
						},
					},
				}},
			},
			want: apis.ErrMultipleOneOf("envFrom.configMapRef", "envFrom.secretRef"),
		}, {
			name: "envFrom - Secret",
			c: corev1.Container{
				Image: "foo",
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "SecretName",
						},
					},
				}},
			},
			want: nil,
		}, {
			name: "envFrom - ConfigMap",
			c: corev1.Container{
				Image: "foo",
				EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "ConfigMapName",
						},
					},
				}},
			},
			want: nil,
		}, {
			name: "termination message policy",
			c: corev1.Container{
				Image:                    "foo",
				TerminationMessagePolicy: "Not a Policy",
			},
			want: apis.ErrInvalidValue(corev1.TerminationMessagePolicy("Not a Policy"), "terminationMessagePolicy"),
		}, {
			name: "empty env var name",
			c: corev1.Container{
				Image: "foo",
				Env: []corev1.EnvVar{{
					Value: "Foo",
				}},
			},
			want: apis.ErrMissingField("env[0].name"),
		}, {
			name: "disallowed envvarsource",
			c: corev1.Container{
				Image: "foo",
				Env: []corev1.EnvVar{{
					Name: "Foo",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "/v1",
						},
					},
				}},
			},
			want: apis.ErrDisallowedFields("env[0].valueFrom.fieldRef"),
		}}
}

func TestVolumeValidation(t *testing.T) {
	tests := []struct {
		name     string
		v        corev1.Volume
		want     *apis.FieldError
		cfgOpts  []configOption
		errLevel apis.DiagnosticLevel
	}{{
		name: "just name",
		v: corev1.Volume{
			Name: "foo",
		},
		want: apis.ErrMissingOneOf("secret", "configMap", "projected", "emptyDir"),
	}, {
		name: "secret volume",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "foo",
				},
			},
		},
	}, {
		name: "configMap volume",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "foo"},
				},
			},
		},
	}, {
		name: "emptyDir volume",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    "Memory",
					SizeLimit: resource.NewQuantity(400, "G"),
				},
			},
		},
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled()},
	}, {
		name: "invalid emptyDir volume",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    "Memory",
					SizeLimit: resource.NewQuantity(-1, "G"),
				},
			},
		},
		want:    apis.ErrInvalidValue(-1, "emptyDir.sizeLimit"),
		cfgOpts: []configOption{withPodSpecVolumesEmptyDirEnabled()},
	}, {
		name: "valid PVC with PVC feature enabled",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "myclaim",
					ReadOnly:  true,
				},
			},
		},
		cfgOpts: []configOption{withPodSpecPersistentVolumeClaimEnabled()},
	}, {
		name: "valid PVC with PVC feature disabled",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "myclaim",
					ReadOnly:  false,
				},
			}},
		want: (&apis.FieldError{
			Message: `Persistent volume claim support is disabled, but found persistent volume claim myclaim`,
		}).Also(&apis.FieldError{
			Message: `Persistent volume write support is disabled, but found persistent volume claim myclaim that is not read-only`,
		}).Also(
			&apis.FieldError{Message: "must not set the field(s)", Paths: []string{"persistentVolumeClaim"}}),
	}, {
		name: "no volume source",
		v: corev1.Volume{
			Name: "foo",
		},
		want: apis.ErrMissingOneOf("secret", "configMap", "projected", "emptyDir"),
	}, {
		name: "multiple volume source",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "foo"},
				},
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "foo",
							},
							Items: []corev1.KeyToPath{{
								Key:  "foo",
								Path: "bar/baz",
							}},
						},
					}},
				},
			},
		},
		want: apis.ErrMultipleOneOf("configMap", "projected"),
	}, {
		name: "multiple projected volumes single source",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "foo",
							},
						},
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "bar",
							},
							Items: []corev1.KeyToPath{{
								Key:  "foo",
								Path: "bar/baz",
							}},
						},
					}},
				},
			},
		},
		want: apis.ErrMultipleOneOf("projected[0].configMap", "projected[0].secret"),
	}, {
		name: "multiple project volume one-per-source",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "foo",
							},
						},
					}, {
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "bar",
							},
						},
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "multiple project volume one-per-source (no names)",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						Secret: &corev1.SecretProjection{},
					}, {
						ConfigMap: &corev1.ConfigMapProjection{},
					}},
				},
			},
		},
		want: apis.ErrMissingField("projected[0].secret.name", "projected[1].configMap.name"),
	}, {
		name: "no project volume source",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{}},
				},
			},
		},
		want: apis.ErrMissingOneOf("projected[0].configMap", "projected[0].downwardAPI", "projected[0].secret", "projected[0].serviceAccountToken"),
	}, {
		name: "no name",
		v: corev1.Volume{
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "foo",
				},
			},
		},
		want: apis.ErrMissingField("name"),
	}, {
		name: "bad name",
		v: corev1.Volume{
			Name: "@@@",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "foo",
				},
			},
		},
		want: apis.ErrInvalidValue("@@@", "name"),
	}, {
		name: "secret missing keyToPath values",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "foo",
					Items:      []corev1.KeyToPath{{}},
				},
			},
		},
		want: apis.ErrMissingField("items[0].key").Also(apis.ErrMissingField("items[0].path")),
	}, {
		name: "configMap missing keyToPath values",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "foo",
					},
					Items: []corev1.KeyToPath{{}},
				},
			},
		},
		want: apis.ErrMissingField("items[0].key").Also(apis.ErrMissingField("items[0].path")),
	}, {
		name: "projection missing keyToPath values",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "foo",
							},
							Items: []corev1.KeyToPath{{}},
						}}, {
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "foo",
							},
							Items: []corev1.KeyToPath{{}},
						}},
					},
				},
			},
		},
		want: apis.ErrMissingField("projected[0].secret.items[0].key").Also(
			apis.ErrMissingField("projected[0].secret.items[0].path")).Also(
			apis.ErrMissingField("projected[1].configMap.items[0].key")).Also(
			apis.ErrMissingField("projected[1].configMap.items[0].path")),
	}, {
		name: "serviceaccounttoken",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience:          "api",
							ExpirationSeconds: ptr.Int64(3600),
							Path:              "token",
						},
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "projection missing serviceaccounttoken values",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience:          "api",
							ExpirationSeconds: ptr.Int64(3600),
						},
					}},
				},
			},
		},
		want: apis.ErrMissingField("projected[0].serviceAccountToken.path"),
	}, {
		name: "projection with DownwardAPI no values",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "labels",
								},
							},
						},
					}},
				},
			},
		},
		want: apis.ErrMissingOneOf("projected[0].downwardAPI.items[0].fieldRef", "projected[0].downwardAPI.items[0].resourceFieldRef"),
	}, {
		name: "projection with DownwardAPI with both fieldRef and resourceFieldRef",
		v: corev1.Volume{
			Name: "foo",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.labels",
									},
									Path: "labels",
									ResourceFieldRef: &corev1.ResourceFieldSelector{
										ContainerName: "user-container",
										Resource:      "limits.cpu",
									},
								},
							},
						},
					}},
				},
			},
		},
		want: apis.ErrGeneric("Within a single item, cannot set both", "projected[0].downwardAPI.items[0].fieldRef", "projected[0].downwardAPI.items[0].resourceFieldRef"),
	},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cfgOpts != nil {
				cfg := config.FromContextOrDefaults(ctx)
				for _, opt := range test.cfgOpts {
					cfg = opt(cfg)
				}
				ctx = config.ToContext(ctx, cfg)
			}
			got := validateVolume(ctx, test.v)
			got = got.Filter(test.errLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateVolume (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestObjectReferenceValidation(t *testing.T) {
	tests := []struct {
		name     string
		r        *corev1.ObjectReference
		want     *apis.FieldError
		errLevel apis.DiagnosticLevel
	}{{
		name: "nil",
	}, {
		name: "no api version",
		r: &corev1.ObjectReference{
			Kind: "Bar",
			Name: "foo",
		},
		want: apis.ErrMissingField("apiVersion"),
	}, {
		name: "bad api version",
		r: &corev1.ObjectReference{
			APIVersion: "/v1alpha1",
			Kind:       "Bar",
			Name:       "foo",
		},
		want: apis.ErrInvalidValue("prefix part must be non-empty", "apiVersion"),
	}, {
		name: "no kind",
		r: &corev1.ObjectReference{
			APIVersion: "foo/v1alpha1",
			Name:       "foo",
		},
		want: apis.ErrMissingField("kind"),
	}, {
		name: "bad kind",
		r: &corev1.ObjectReference{
			APIVersion: "foo/v1alpha1",
			Kind:       "Bad Kind",
			Name:       "foo",
		},
		want: apis.ErrInvalidValue("a valid C identifier must start with alphabetic character or '_', followed by a string of alphanumeric characters or '_' (e.g. 'my_name',  or 'MY_NAME',  or 'MyName', regex used for validation is '[A-Za-z_][A-Za-z0-9_]*')", "kind"),
	}, {
		name: "no namespace",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
			Name:       "the-bar-0001",
		},
		want: nil,
	}, {
		name: "no name",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
		},
		want: apis.ErrMissingField("name"),
	}, {
		name: "bad name",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
			Name:       "bad name",
		},
		want: apis.ErrInvalidValue("a lowercase RFC 1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')", "name"),
	}, {
		name: "disallowed fields",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
			Name:       "bar0001",

			// None of these are allowed.
			Namespace:       "foo",
			FieldPath:       "some.field.path",
			ResourceVersion: "234234",
			UID:             "deadbeefcafebabe",
		},
		want: apis.ErrDisallowedFields("namespace", "fieldPath", "resourceVersion", "uid"),
	}, {
		name: "all good",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
			Name:       "bar0001",
		},
		want: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ValidateNamespacedObjectReference(test.r)
			got = got.Filter(test.errLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidateNamespacedObjectReference (-want, +got): \n%s", diff)
			}
		})
	}
}

func TestPodSpecSecurityContextValidation(t *testing.T) {
	// Note the feature flag is always enabled on this test
	tests := []struct {
		name     string
		sc       *corev1.PodSecurityContext
		want     *apis.FieldError
		errLevel apis.DiagnosticLevel
	}{{
		name: "nil",
	}, {
		name: "disallowed fields",
		sc: &corev1.PodSecurityContext{
			SELinuxOptions: &corev1.SELinuxOptions{},
			WindowsOptions: &corev1.WindowsSecurityContextOptions{},
			Sysctls:        []corev1.Sysctl{},
		},
		want: apis.ErrDisallowedFields("seLinuxOptions", "sysctls", "windowsOptions"),
	}, {
		name: "too large uid",
		sc: &corev1.PodSecurityContext{
			RunAsUser: ptr.Int64(math.MaxInt32 + 1),
		},
		want: apis.ErrOutOfBoundsValue(int64(math.MaxInt32+1), 0, math.MaxInt32, "runAsUser"),
	}, {
		name: "negative uid",
		sc: &corev1.PodSecurityContext{
			RunAsUser: ptr.Int64(-10),
		},
		want: apis.ErrOutOfBoundsValue(-10, 0, math.MaxInt32, "runAsUser"),
	}, {
		name: "too large gid",
		sc: &corev1.PodSecurityContext{
			RunAsGroup: ptr.Int64(math.MaxInt32 + 1),
		},
		want: apis.ErrOutOfBoundsValue(int64(math.MaxInt32+1), 0, math.MaxInt32, "runAsGroup"),
	}, {
		name: "negative gid",
		sc: &corev1.PodSecurityContext{
			RunAsGroup: ptr.Int64(-10),
		},
		want: apis.ErrOutOfBoundsValue(-10, 0, math.MaxInt32, "runAsGroup"),
	}, {
		name: "too large fsGroup",
		sc: &corev1.PodSecurityContext{
			FSGroup: ptr.Int64(math.MaxInt32 + 1),
		},
		want: apis.ErrOutOfBoundsValue(int64(math.MaxInt32+1), 0, math.MaxInt32, "fsGroup"),
	}, {
		name: "negative fsGroup",
		sc: &corev1.PodSecurityContext{
			FSGroup: ptr.Int64(-10),
		},
		want: apis.ErrOutOfBoundsValue(-10, 0, math.MaxInt32, "fsGroup"),
	}, {
		name: "too large supplementalGroups",
		sc: &corev1.PodSecurityContext{
			SupplementalGroups: []int64{int64(math.MaxInt32 + 1)},
		},
		want: apis.ErrOutOfBoundsValue(int64(math.MaxInt32+1), 0, math.MaxInt32, "supplementalGroups[0]"),
	}, {
		name: "negative supplementalGroups",
		sc: &corev1.PodSecurityContext{
			SupplementalGroups: []int64{-10},
		},
		want: apis.ErrOutOfBoundsValue(-10, 0, math.MaxInt32, "supplementalGroups[0]"),
	}}

	for _, test := range tests {
		ctx := config.ToContext(context.Background(),
			&config.Config{
				Features: &config.Features{
					PodSpecSecurityContext: config.Enabled,
				},
			})

		t.Run(test.name, func(t *testing.T) {
			got := ValidatePodSecurityContext(ctx, test.sc)
			got.Filter(test.errLevel)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("ValidatePodSecurityContext(-want, +got): \n%s", diff)
			}
		})
	}
}
