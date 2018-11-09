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
	"github.com/google/go-cmp/cmp"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/knative/pkg/apis"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func (rt *PodAutoscaler) Validate() *apis.FieldError {
	return servingv1alpha1.ValidateObjectMetadata(rt.GetObjectMeta()).ViaField("metadata").Also(rt.Spec.Validate().ViaField("spec"))
}

func (rs *PodAutoscalerSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &PodAutoscalerSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	errs := validateReference(rs.ScaleTargetRef).ViaField("scaleTargetRef")
	if rs.ServiceName == "" {
		errs = errs.Also(apis.ErrMissingField("serviceName"))
	}
	if err := rs.ConcurrencyModel.Validate(); err != nil {
		errs = errs.Also(err.ViaField("concurrencyModel"))
	} else if err := servingv1alpha1.ValidateContainerConcurrency(rs.ContainerConcurrency, rs.ConcurrencyModel); err != nil {
		errs = errs.Also(err)
	}
	return errs
}

func validateReference(ref autoscalingv1.CrossVersionObjectReference) *apis.FieldError {
	if equality.Semantic.DeepEqual(ref, autoscalingv1.CrossVersionObjectReference{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var errs *apis.FieldError
	if ref.Kind == "" {
		errs = errs.Also(apis.ErrMissingField("kind"))
	}
	if ref.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}
	if ref.APIVersion == "" {
		errs = errs.Also(apis.ErrMissingField("apiVersion"))
	}
	return errs
}

func (current *PodAutoscaler) CheckImmutableFields(og apis.Immutable) *apis.FieldError {
	original, ok := og.(*PodAutoscaler)
	if !ok {
		return &apis.FieldError{Message: "The provided original was not a PodAutoscaler"}
	}

	if diff := cmp.Diff(original.Spec, current.Spec); diff != "" {
		return &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: diff,
		}
	}
	return nil
}
