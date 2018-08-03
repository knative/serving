/*
Copyright 2017 The Knative Authors

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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/knative/pkg/apis"
)

func TestContainerValidation(t *testing.T) {
	tests := []struct {
		name string
		c    corev1.Container
		want *apis.FieldError
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
		name: "has a name",
		c: corev1.Container{
			Name: "foo",
		},
		want: apis.ErrDisallowedFields("name"),
	}, {
		name: "has resources",
		c: corev1.Container{
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				},
			},
		},
		want: apis.ErrDisallowedFields("resources"),
	}, {
		name: "has ports",
		c: corev1.Container{
			Ports: []corev1.ContainerPort{{
				Name:          "http",
				ContainerPort: 8080,
			}},
		},
		want: apis.ErrDisallowedFields("ports"),
	}, {
		name: "has volumeMounts",
		c: corev1.Container{
			VolumeMounts: []corev1.VolumeMount{{
				MountPath: "mount/path",
				Name:      "name",
			}},
		},
		want: apis.ErrDisallowedFields("volumeMounts"),
	}, {
		name: "has lifecycle",
		c: corev1.Container{
			Lifecycle: &corev1.Lifecycle{},
		},
		want: apis.ErrDisallowedFields("lifecycle"),
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
			Name: "foo",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("cpu"): resource.MustParse("25m"),
				},
			},
			Ports: []corev1.ContainerPort{{
				Name:          "http",
				ContainerPort: 8080,
			}},
			VolumeMounts: []corev1.VolumeMount{{
				MountPath: "mount/path",
				Name:      "name",
			}},
			Lifecycle: &corev1.Lifecycle{},
		},
		want: apis.ErrDisallowedFields("name", "resources", "ports", "volumeMounts", "lifecycle"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validateContainer(test.c)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
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
			got := test.cm.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestServingStateValidation(t *testing.T) {
	tests := []struct {
		name string
		ss   RevisionServingStateType
		want *apis.FieldError
	}{{
		name: "active",
		ss:   "Active",
		want: nil,
	}, {
		name: "reserve",
		ss:   "Reserve",
		want: nil,
	}, {
		name: "retired",
		ss:   "Retired",
		want: nil,
	}, {
		name: "empty",
		ss:   "",
		want: nil,
	}, {
		name: "bogus",
		ss:   "bogus",
		want: apis.ErrInvalidValue("bogus", apis.CurrentField),
	}, {
		name: "balderdash",
		ss:   "balderdash",
		want: apis.ErrInvalidValue("balderdash", apis.CurrentField),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.ss.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
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
			ConcurrencyModel: "Multi",
		},
		want: nil,
	}, {
		name: "has bad serving state",
		rs: &RevisionSpec{
			ServingState: "blah",
		},
		want: apis.ErrInvalidValue("blah", "servingState"),
	}, {
		name: "bad concurrency model",
		rs: &RevisionSpec{
			Container: corev1.Container{
				Image: "helloworld",
			},
			ConcurrencyModel: "bogus",
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
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rs.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
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
				ConcurrencyModel: "Multi",
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
				ConcurrencyModel: "Multi",
			},
		},
		want: apis.ErrDisallowedFields("spec.container.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rts.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
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
			Spec: RevisionSpec{
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		want: nil,
	}, {
		name: "empty spec",
		r:    &Revision{},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		r: &Revision{
			Spec: RevisionSpec{
				Container: corev1.Container{
					Name:  "kevin",
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		want: apis.ErrDisallowedFields("spec.container.name"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

type notARevision struct{}

func (nar *notARevision) CheckImmutableFields(apis.Immutable) *apis.FieldError {
	return nil
}

func TestImmutableFields(t *testing.T) {
	tests := []struct {
		name string
		new  apis.Immutable
		old  apis.Immutable
		want *apis.FieldError
	}{{
		name: "good (no change)",
		new: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		want: nil,
	}, {
		name: "good (serving state change)",
		new: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			Spec: RevisionSpec{
				ServingState: "Reserve",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		want: nil,
	}, {
		name: "bad (type mismatch)",
		new: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		old:  &notARevision{},
		want: &apis.FieldError{Message: "The provided original was not a Revision"},
	}, {
		name: "bad (container image change)",
		new: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "busybox",
				},
				ConcurrencyModel: "Multi",
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
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Single",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.ConcurrencyModel:
	-: v1alpha1.RevisionRequestConcurrencyModelType("Single")
	+: v1alpha1.RevisionRequestConcurrencyModelType("Multi")
`,
		},
	}, {
		name: "bad (multiple changes)",
		new: &Revision{
			Spec: RevisionSpec{
				ServingState: "Active",
				Container: corev1.Container{
					Image: "helloworld",
				},
				ConcurrencyModel: "Multi",
			},
		},
		old: &Revision{
			Spec: RevisionSpec{
				ServingState: "Reserve",
				Container: corev1.Container{
					Image: "busybox",
				},
				ConcurrencyModel: "Single",
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.RevisionSpec}.ConcurrencyModel:
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
			got := test.new.CheckImmutableFields(test.old)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
