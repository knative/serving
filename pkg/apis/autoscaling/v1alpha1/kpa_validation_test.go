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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
)

func TestPodAutoscalerSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rs   *PodAutoscalerSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ServiceName:          "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: nil,
	}, {
		name: "has missing scaleTargetRef",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ServiceName:          "foo",
		},
		want: apis.ErrMissingField("scaleTargetRef"),
	}, {
		name: "has missing scaleTargetRef kind",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ServiceName:          "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Name:       "bar",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.kind"),
	}, {
		name: "has missing scaleTargetRef apiVersion",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ServiceName:          "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "bar",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.apiVersion"),
	}, {
		name: "has missing scaleTargetRef name",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ServiceName:          "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.name"),
	}, {
		name: "has missing serviceName",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrMissingField("serviceName"),
	}, {
		name: "bad concurrency model",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel: "bogus",
			ServiceName:      "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrInvalidValue("bogus", "concurrencyModel"),
	}, {
		name: "bad container concurrency",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: -1,
			ServiceName:          "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrInvalidValue("-1", "containerConcurrency"),
	}, {
		name: "bad concurrency model and container concurrency combination",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel:     "Single",
			ContainerConcurrency: 0,
			ServiceName:          "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel"),
	}, {
		name: "multi invalid, bad concurrency model and missing ref kind",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: -0,
			ServiceName:          "foo",
			ConcurrencyModel:     "super-bogus",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Name:       "bar",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.kind").
			Also(apis.ErrInvalidValue("super-bogus", "concurrencyModel")),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rs.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestPodAutoscalerValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *PodAutoscaler
		want *apis.FieldError
	}{{
		name: "valid",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{
					"minScale": "2",
				},
			},
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		want: nil,
	}, {
		name: "bad scale bounds",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "FOO",
				},
			},
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		want: (&apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be integer greater than 0", autoscaling.MinScaleAnnotationKey),
			Paths:   []string{autoscaling.MinScaleAnnotationKey},
		}).ViaField("annotations").ViaField("metadata"),
	}, {
		name: "empty spec",
		r:    &PodAutoscaler{},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		r: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "BadValue",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		want: apis.ErrInvalidValue("BadValue", "spec.concurrencyModel"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate()
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

type notAPodAutoscaler struct{}

func (nar *notAPodAutoscaler) CheckImmutableFields(apis.Immutable) *apis.FieldError {
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
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		old: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		want: nil,
	}, {
		name: "bad (type mismatch)",
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		old:  &notAPodAutoscaler{},
		want: &apis.FieldError{Message: "The provided original was not a PodAutoscaler"},
	}, {
		name: "bad (concurrency model change)",
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		old: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Single",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.PodAutoscalerSpec}.ConcurrencyModel:
	-: v1alpha1.RevisionRequestConcurrencyModelType("Single")
	+: v1alpha1.RevisionRequestConcurrencyModelType("Multi")
`,
		},
	}, {
		name: "bad (container concurrency change)",
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ContainerConcurrency: 0,
				ServiceName:          "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		old: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ContainerConcurrency: 1,
				ServiceName:          "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.PodAutoscalerSpec}.ContainerConcurrency:
	-: v1alpha1.RevisionContainerConcurrencyType(1)
	+: v1alpha1.RevisionContainerConcurrencyType(0)
`,
		},
	}, {
		name: "bad (multiple changes)",
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Multi",
				ServiceName:      "foo",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
			},
		},
		old: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ConcurrencyModel: "Single",
				ServiceName:      "food",
				ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "baz",
				},
			},
		},
		want: &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: `{v1alpha1.PodAutoscalerSpec}.ConcurrencyModel:
	-: v1alpha1.RevisionRequestConcurrencyModelType("Single")
	+: v1alpha1.RevisionRequestConcurrencyModelType("Multi")
{v1alpha1.PodAutoscalerSpec}.ScaleTargetRef.Name:
	-: "baz"
	+: "bar"
{v1alpha1.PodAutoscalerSpec}.ServiceName:
	-: "food"
	+: "foo"
`,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.new.CheckImmutableFields(test.old)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
