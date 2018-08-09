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
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/knative/pkg/apis"
)

func (rt *PodAutoscaler) Validate() *apis.FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rs *PodAutoscalerSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &PodAutoscalerSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	if err := validateReference(rs.ScaleTargetRef); err != nil {
		return err.ViaField("scaleTargetRef")
	}
	if rs.ServiceName == "" {
		return apis.ErrMissingField("serviceName")
	}
	if err := rs.ServingState.Validate(); err != nil {
		return err.ViaField("servingState")
	}
	return rs.ConcurrencyModel.Validate().ViaField("concurrencyModel")
}

func validateReference(ref autoscalingv1.CrossVersionObjectReference) *apis.FieldError {
	if equality.Semantic.DeepEqual(ref, autoscalingv1.CrossVersionObjectReference{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	if ref.Kind == "" {
		return apis.ErrMissingField("kind")
	}
	if ref.Name == "" {
		return apis.ErrMissingField("name")
	}
	if ref.APIVersion == "" {
		return apis.ErrMissingField("apiVersion")
	}
	return nil
}

func (current *PodAutoscaler) CheckImmutableFields(og apis.Immutable) *apis.FieldError {
	original, ok := og.(*PodAutoscaler)
	if !ok {
		return &apis.FieldError{Message: "The provided original was not a PodAutoscaler"}
	}

	// The autoscaler is allowed to change ServingState, but consider the rest.
	ignoreServingState := cmpopts.IgnoreFields(PodAutoscalerSpec{}, "ServingState")
	if diff := cmp.Diff(original.Spec, current.Spec, ignoreServingState); diff != "" {
		return &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: diff,
		}
	}
	return nil
}
