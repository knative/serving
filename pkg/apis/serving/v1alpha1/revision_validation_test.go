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

package v1alpha1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	net "github.com/knative/serving/pkg/apis/networking"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestContainerValidation(t *testing.T) {
	bidir := corev1.MountPropagationBidirectional

	tests := []struct {
		name    string
		c       corev1.Container
		want    *apis.FieldError
		volumes sets.String
	}{{
		name: "empty container",
		c:    corev1.Container{},
		want: apis.ErrMissingField(apis.CurrentField),
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
			Details: "image: \"foo:bar:baz\", error: could not parse reference",
		},
	}, {
		name: "has a name",
		c: corev1.Container{
			Name:  "foo",
			Image: "foo",
		},
		want: apis.ErrDisallowedFields("name"),
	}, {
		name: "has resources",
		c: corev1.Container{
			Image: "foo",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
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
		name: "has valid user port http1",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				Name:          "http1",
				ContainerPort: 8081,
			}},
		},
		want: nil,
	}, {
		name: "has valid user port h2c",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				Name:          "h2c",
				ContainerPort: 8081,
			}},
		},
		want: nil,
	}, {
		name: "has more than one ports with valid names",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				Name:          "h2c",
				ContainerPort: 8080,
			}, {
				Name:          "http1",
				ContainerPort: 8181,
			}},
		},
		want: &apis.FieldError{
			Message: "More than one container port is set",
			Paths:   []string{"ports"},
			Details: "Only a single port is allowed",
		},
	}, {
		name: "has container port value too large",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				ContainerPort: 65536,
			}},
		},
		want: apis.ErrOutOfBoundsValue(65536, 1, 65535, "ports.ContainerPort"),
	}, {
		name: "has an empty port set",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{}},
		},
		want: apis.ErrOutOfBoundsValue(0, 1, 65535, "ports.ContainerPort"),
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
			Message: "More than one container port is set",
			Paths:   []string{"ports"},
			Details: "Only a single port is allowed",
		},
	}, {
		name: "has tcp protocol",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 8080,
			}},
		},
		want: nil,
	}, {
		name: "has invalid protocol",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				Protocol:      "tdp",
				ContainerPort: 8080,
			}},
		},
		want: apis.ErrInvalidValue("tdp", "ports.Protocol"),
	}, {
		name: "has host port",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				HostPort:      80,
				ContainerPort: 8080,
			}},
		},
		want: apis.ErrDisallowedFields("ports.HostPort"),
	}, {
		name: "has host ip",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				HostIP:        "127.0.0.1",
				ContainerPort: 8080,
			}},
		},
		want: apis.ErrDisallowedFields("ports.HostIP"),
	}, {
		name: "port conflicts with queue proxy admin",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8022,
			}},
		},
		want: apis.ErrInvalidValue(8022, "ports.ContainerPort"),
	}, {
		name: "port conflicts with queue proxy",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				ContainerPort: 8012,
			}},
		},
		want: apis.ErrInvalidValue(8012, "ports.ContainerPort"),
	}, {
		name: "port conflicts with queue proxy metrics",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				ContainerPort: 9090,
			}},
		},
		want: apis.ErrInvalidValue(9090, "ports.ContainerPort"),
	}, {
		name: "has invalid port name",
		c: corev1.Container{
			Image: "foo",
			Ports: []corev1.ContainerPort{{
				Name:          "foobar",
				ContainerPort: 8080,
			}},
		},
		want: &apis.FieldError{
			Message: fmt.Sprintf("Port name %v is not allowed", "foobar"),
			Paths:   []string{"ports"},
			Details: "Name must be empty, or one of: 'h2c', 'http1'",
		},
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
			apis.ErrMissingField("readOnly").ViaFieldIndex("volumeMounts", 0)).Also(
			apis.ErrMissingField("mountPath").ViaFieldIndex("volumeMounts", 0)).Also(
			apis.ErrDisallowedFields("subPath").ViaFieldIndex("volumeMounts", 0)).Also(
			apis.ErrDisallowedFields("mountPropagation").ViaFieldIndex("volumeMounts", 0)),
	}, {
		name: "missing known volumeMounts",
		c: corev1.Container{
			Image: "foo",
		},
		volumes: sets.NewString("the-name"),
		want: &apis.FieldError{
			Message: "volumes not mounted: [the-name]",
			Paths:   []string{"volumeMounts"},
		},
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
		volumes: sets.NewString("the-name"),
	}, {
		name: "has known volumeMounts, but at reserved path",
		c: corev1.Container{
			Image: "foo",
			VolumeMounts: []corev1.VolumeMount{{
				MountPath: "//var//log//",
				Name:      "the-name",
				ReadOnly:  true,
			}},
		},
		volumes: sets.NewString("the-name"),
		want: (&apis.FieldError{
			Message: `mountPath "/var/log" is a reserved path`,
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
		volumes: sets.NewString("the-name"),
		want:    apis.ErrInvalidValue("not/absolute", "volumeMounts[0].mountPath"),
	}, {
		name: "has lifecycle",
		c: corev1.Container{
			Image:     "foo",
			Lifecycle: &corev1.Lifecycle{},
		},
		want: apis.ErrDisallowedFields("lifecycle"),
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
		volumes: sets.NewString("the-name"),
	}, {
		name: "valid with probes (no port)",
		c: corev1.Container{
			Image: "foo",
			ReadinessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid readiness http probe (has port)",
		c: corev1.Container{
			Image: "foo",
			ReadinessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt(8080),
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("readinessProbe.httpGet.port"),
	}, {
		name: "invalid liveness tcp probe (has port)",
		c: corev1.Container{
			Image: "foo",
			LivenessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromString("http"),
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("livenessProbe.tcpSocket.port"),
	}, {
		name: "has numerous problems",
		c: corev1.Container{
			Name:      "foo",
			Lifecycle: &corev1.Lifecycle{},
		},
		want: apis.ErrDisallowedFields("name", "lifecycle").Also(
			&apis.FieldError{
				Message: "Failed to parse image reference",
				Paths:   []string{"image"},
				Details: "image: \"\", error: could not parse reference",
			},
		),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validateContainer(test.c, test.volumes)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestVolumeValidation(t *testing.T) {
	tests := []struct {
		name string
		v    corev1.Volume
		want *apis.FieldError
	}{{
		name: "just name",
		v: corev1.Volume{
			Name: "foo",
		},
		want: apis.ErrMissingOneOf("secret", "configMap"),
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
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		want: apis.ErrMissingOneOf("secret", "configMap"),
	}, {
		name: "no volume source",
		v: corev1.Volume{
			Name: "foo",
		},
		want: apis.ErrMissingOneOf("secret", "configMap"),
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
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validateVolume(test.v)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateVolume (-want, +got) = %v", diff)
			}
		})
	}
}

func TestBuildRefValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *corev1.ObjectReference
		want *apis.FieldError
	}{{
		name: "nil",
	}, {
		name: "no api version",
		r:    &corev1.ObjectReference{},
		want: apis.ErrInvalidValue("", "apiVersion"),
	}, {
		name: "bad api version",
		r: &corev1.ObjectReference{
			APIVersion: "/v1alpha1",
		},
		want: apis.ErrInvalidValue("/v1alpha1", "apiVersion"),
	}, {
		name: "no kind",
		r: &corev1.ObjectReference{
			APIVersion: "foo/v1alpha1",
		},
		want: apis.ErrInvalidValue("", "kind"),
	}, {
		name: "bad kind",
		r: &corev1.ObjectReference{
			APIVersion: "foo/v1alpha1",
			Kind:       "Bad Kind",
		},
		want: apis.ErrInvalidValue("Bad Kind", "kind"),
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
		want: apis.ErrInvalidValue("", "name"),
	}, {
		name: "bad name",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
			Name:       "bad name",
		},
		want: apis.ErrInvalidValue("bad name", "name"),
	}, {
		name: "disallowed fields",
		r: &corev1.ObjectReference{
			APIVersion: "foo.group/v1alpha1",
			Kind:       "Bar",
			Name:       "bar0001",

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
			got := validateBuildRef(test.r)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateBuildRef (-want, +got) = %v", diff)
			}
		})
	}
}

func TestConcurrencyModelValidation(t *testing.T) {
	tests := []struct {
		name string
		cm   RevisionRequestConcurrencyModelType
		want *apis.FieldError
	}{{
		name: "single",
		cm:   RevisionRequestConcurrencyModelSingle,
		want: nil,
	}, {
		name: "multi",
		cm:   RevisionRequestConcurrencyModelMulti,
		want: nil,
	}, {
		name: "empty",
		cm:   "",
		want: nil,
	}, {
		name: "bogus",
		cm:   "bogus",
		want: apis.ErrInvalidValue("bogus", apis.CurrentField),
	}, {
		name: "balderdash",
		cm:   "balderdash",
		want: apis.ErrInvalidValue("balderdash", apis.CurrentField),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.cm.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestContainerConcurrencyValidation(t *testing.T) {
	tests := []struct {
		name string
		cc   RevisionContainerConcurrencyType
		cm   RevisionRequestConcurrencyModelType
		want *apis.FieldError
	}{{
		name: "single with only container concurrency",
		cc:   1,
		cm:   RevisionRequestConcurrencyModelType(""),
		want: nil,
	}, {
		name: "single with container currency and concurrency model",
		cc:   1,
		cm:   RevisionRequestConcurrencyModelSingle,
		want: nil,
	}, {
		name: "multi with only container concurrency",
		cc:   0,
		cm:   RevisionRequestConcurrencyModelType(""),
		want: nil,
	}, {
		name: "multi with container concurrency and concurrency model",
		cc:   0,
		cm:   RevisionRequestConcurrencyModelMulti,
		want: nil,
	}, {
		name: "mismatching container concurrency (1) and concurrency model (multi)",
		cc:   1,
		cm:   RevisionRequestConcurrencyModelMulti,
		want: apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel"),
	}, {
		name: "mismatching container concurrency (0) and concurrency model (single)",
		cc:   0,
		cm:   RevisionRequestConcurrencyModelSingle,
		want: apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel"),
	}, {
		name: "invalid container concurrency (too small)",
		cc:   -1,
		want: apis.ErrInvalidValue(-1, "containerConcurrency"),
	}, {
		name: "invalid container concurrency (too large)",
		cc:   RevisionContainerConcurrencyMax + 1,
		want: apis.ErrInvalidValue(int(RevisionContainerConcurrencyMax)+1, "containerConcurrency"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ValidateContainerConcurrency(test.cc, test.cm)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rs   *RevisionSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
			},
			DeprecatedConcurrencyModel: "Multi",
		},
		want: nil,
	}, {
		name: "with volume (ok)",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
			Volumes: []corev1.Volume{{
				Name: "the-name",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "foo",
					},
				},
			}},
			DeprecatedConcurrencyModel: "Multi",
		},
		want: nil,
	}, {
		name: "with volume name collision",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mount/path",
					Name:      "the-name",
					ReadOnly:  true,
				}},
			},
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
			DeprecatedConcurrencyModel: "Multi",
		},
		want: (&apis.FieldError{
			Message: fmt.Sprintf(`duplicate volume name "the-name"`),
			Paths:   []string{"name"},
		}).ViaFieldIndex("volumes", 1),
	}, {
		name: "has bad build ref",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
			},
			BuildRef: &corev1.ObjectReference{},
		},
		want: apis.ErrInvalidValue("", "buildRef.apiVersion"),
	}, {
		name: "bad concurrency model",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
			},
			DeprecatedConcurrencyModel: "bogus",
		},
		want: apis.ErrInvalidValue("bogus", "concurrencyModel"),
	}, {
		name: "bad container spec",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Name:  "steve",
				Image: "helloworld",
			},
		},
		want: apis.ErrDisallowedFields("container.name"),
	}, {
		name: "exceed max timeout",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
			},
			TimeoutSeconds: 6000,
		},
		want: apis.ErrOutOfBoundsValue(6000, 0,
			net.DefaultTimeout.Seconds(),
			"timeoutSeconds"),
	}, {
		name: "negative timeout",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
			},
			TimeoutSeconds: -30,
		},
		want: apis.ErrOutOfBoundsValue(-30, 0,
			net.DefaultTimeout.Seconds(),
			"timeoutSeconds"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rs.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionTemplateSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rts  *RevisionTemplateSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rts: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: nil,
	}, {
		name: "empty spec",
		rts:  &RevisionTemplateSpec{},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		rts: &RevisionTemplateSpec{
			Spec: RevisionSpec{
				Container: corev1.Container{
					Name:  "kevin",
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: apis.ErrDisallowedFields("spec.container.name"),
	}, {
		name: "has revision template name",
		rts: &RevisionTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: apis.ErrDisallowedFields("metadata.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rts.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *Revision
		want *apis.FieldError
	}{{
		name: "valid",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: nil,
	}, {
		name: "empty spec",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
		},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Name:  "kevin",
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: apis.ErrDisallowedFields("spec.container.name"),
	}, {
		name: "invalid name - dots and too long",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "a" + strings.Repeat(".", 62) + "a",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: &apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "invalid metadata.annotations - scale bounds",
		r: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "scale-bounds",
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "5",
					autoscaling.MaxScaleAnnotationKey: "2",
				},
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: (&apis.FieldError{
			Message: fmt.Sprintf("%s=%v is less than %s=%v", autoscaling.MaxScaleAnnotationKey, 2, autoscaling.MinScaleAnnotationKey, 5),
			Paths:   []string{autoscaling.MaxScaleAnnotationKey, autoscaling.MinScaleAnnotationKey},
		}).ViaField("annotations").ViaField("metadata"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestImmutableFields(t *testing.T) {
	tests := []struct {
		name string
		new  *Revision
		old  *Revision
		want *apis.FieldError
	}{{
		name: "good (no change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: nil,
	}, {
		name: "bad (resources image change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("cpu"): resource.MustParse("50m"),
						},
					},
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("cpu"): resource.MustParse("100m"),
						},
					},
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.Container.Resources.Requests["cpu"]:
	-: resource.Quantity{i: resource.int64Amount{value: 100, scale: resource.Scale(-3)}, s: "100m", Format: resource.Format("DecimalSI")}
	+: resource.Quantity{i: resource.int64Amount{value: 50, scale: resource.Scale(-3)}, s: "50m", Format: resource.Format("DecimalSI")}
`,
		},
	}, {
		name: "bad (container image change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.Container.Image:
	-: "busybox"
	+: "helloworld"
`,
		},
	}, {
		name: "bad (concurrency model change)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Single",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.DeprecatedConcurrencyModel:
	-: v1alpha1.RevisionRequestConcurrencyModelType("Single")
	+: v1alpha1.RevisionRequestConcurrencyModelType("Multi")
`,
		},
	}, {
		name: "bad (new field added)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.DeprecatedConcurrencyModel:
	-: v1alpha1.RevisionRequestConcurrencyModelType("")
	+: v1alpha1.RevisionRequestConcurrencyModelType("Multi")
`,
		},
	}, {
		name: "bad (multiple changes)",
		new: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				DeprecatedConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
				DeprecatedConcurrencyModel: "Single",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.DeprecatedConcurrencyModel:
	-: v1alpha1.RevisionRequestConcurrencyModelType("Single")
	+: v1alpha1.RevisionRequestConcurrencyModelType("Multi")
{v1alpha1.RevisionSpec}.Container.Image:
	-: "busybox"
	+: "helloworld"
`,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.old)
			got := test.new.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionProtocolType(t *testing.T) {
	tests := []struct {
		p    net.ProtocolType
		want *apis.FieldError
	}{{
		net.ProtocolH2C, nil,
	}, {
		net.ProtocolHTTP1, nil,
	}, {
		net.ProtocolType(""), apis.ErrInvalidValue("", apis.CurrentField),
	}, {
		net.ProtocolType("token-ring"), apis.ErrInvalidValue("token-ring", apis.CurrentField),
	}}
	for _, test := range tests {
		e := test.p.Validate()
		if got, want := e.Error(), test.want.Error(); !cmp.Equal(got, want) {
			t.Errorf("Got = %v, want: %v, diff: %s", got, want, cmp.Diff(got, want))
		}
	}
}
