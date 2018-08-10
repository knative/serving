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
	autoscalingv1 "k8s.io/api/autoscaling/v1"

	"github.com/knative/pkg/apis"
)

func TestPodAutoscalerSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rs   *PodAutoscalerSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel: "Multi",
			ServiceName:      "foo",
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
			ConcurrencyModel: "Multi",
			ServiceName:      "foo",
		},
		want: apis.ErrMissingField("scaleTargetRef"),
	}, {
		name: "has missing scaleTargetRef kind",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel: "Multi",
			ServiceName:      "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Name:       "bar",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.kind"),
	}, {
		name: "has missing scaleTargetRef apiVersion",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel: "Multi",
			ServiceName:      "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "bar",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.apiVersion"),
	}, {
		name: "has missing scaleTargetRef name",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel: "Multi",
			ServiceName:      "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
		},
		want: apis.ErrMissingField("scaleTargetRef.name"),
	}, {
		name: "has missing serviceName",
		rs: &PodAutoscalerSpec{
			ConcurrencyModel: "Multi",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrMissingField("serviceName"),
	}, {
		name: "has bad serving state",
		rs: &PodAutoscalerSpec{
			ServingState: "blah",
			ServiceName:  "foo",
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrInvalidValue("blah", "servingState"),
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

func TestPodAutoscalerValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *PodAutoscaler
		want *apis.FieldError
	}{{
		name: "valid",
		r: &PodAutoscaler{
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
			if diff := cmp.Diff(test.want, got); diff != "" {
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
				ServingState:     "Active",
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
				ServingState:     "Active",
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
		name: "good (serving state change)",
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ServingState:     "Active",
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
				ServingState:     "Reserve",
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
				ServingState:     "Active",
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
				ServingState:     "Active",
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
				ServingState:     "Active",
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
		name: "bad (multiple changes)",
		new: &PodAutoscaler{
			Spec: PodAutoscalerSpec{
				ServingState:     "Active",
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
				ServingState:     "Reserve",
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
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
