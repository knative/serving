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

package v1beta1

import (
	"context"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/kmp"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
)

// Validate ensures Revision is properly configured.
func (r *Revision) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(r.GetObjectMeta()).ViaField("metadata")
	errs = errs.Also(r.Spec.Validate(withinSpec(ctx)).ViaField("spec"))
	errs = errs.Also(r.Status.Validate(withinStatus(ctx)).ViaField("status"))

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*Revision)
		if diff, err := kmp.SafeDiff(original.Spec, r.Spec); err != nil {
			return &apis.FieldError{
				Message: "Failed to diff Revision",
				Paths:   []string{"spec"},
				Details: err.Error(),
			}
		} else if diff != "" {
			return &apis.FieldError{
				Message: "Immutable fields changed (-old +new)",
				Paths:   []string{"spec"},
				Details: diff,
			}
		}
	}

	return errs
}

// Validate implements apis.Validatable
func (rts *RevisionTemplateSpec) Validate(ctx context.Context) *apis.FieldError {
	// TODO(mattmoor): Specialized ObjectMeta validation.
	return rts.Spec.Validate(withinSpec(ctx)).ViaField("spec")
}

// Validate implements apis.Validatable
func (rs *RevisionSpec) Validate(ctx context.Context) *apis.FieldError {
	err := rs.ContainerConcurrency.Validate(ctx).ViaField("containerConcurrency")

	// TODO(dangerd): Validate pod spec.

	if rs.TimeoutSeconds != nil {
		ts := *rs.TimeoutSeconds
		max := int64(networking.DefaultTimeout.Seconds())
		if ts < 0 || ts > max {
			err = err.Also(apis.ErrOutOfBoundsValue(
				ts, 0, max, "timeoutSeconds"))
		}
	}

	return err
}

// Validate implements apis.Validatable.
func (cc RevisionContainerConcurrencyType) Validate(ctx context.Context) *apis.FieldError {
	if cc < 0 || cc > RevisionContainerConcurrencyMax {
		return apis.ErrOutOfBoundsValue(
			cc, 0, RevisionContainerConcurrencyMax, apis.CurrentField)
	}
	return nil
}

// Validate implements apis.Validatable
func (rs *RevisionStatus) Validate(ctx context.Context) *apis.FieldError {
	return nil
}
