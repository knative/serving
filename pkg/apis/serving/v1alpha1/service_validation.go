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
	"github.com/knative/pkg/apis"
)

func (s *Service) Validate() *apis.FieldError {
	return ValidateObjectMetadata(s.GetObjectMeta()).ViaField("metadata").
		Also(s.Spec.Validate().ViaField("spec"))
}

func (ss *ServiceSpec) Validate() *apis.FieldError {
	// We would do this semantic DeepEqual, but the spec is comprised
	// entirely of a oneof, the validation for which produces a clearer
	// error message.
	// if equality.Semantic.DeepEqual(ss, &ServiceSpec{}) {
	// 	return apis.ErrMissingField(currentField)
	// }

	switch {
	case ss.RunLatest != nil && ss.Pinned != nil:
		return apis.ErrMultipleOneOf("runLatest", "pinned")
	case ss.RunLatest != nil:
		return ss.RunLatest.Validate().ViaField("runLatest")
	case ss.Pinned != nil:
		return ss.Pinned.Validate().ViaField("pinned")
	default:
		return apis.ErrMissingOneOf("runLatest", "pinned")
	}
}

func (pt *PinnedType) Validate() *apis.FieldError {
	var errs *apis.FieldError
	if pt.RevisionName == "" {
		errs = apis.ErrMissingField("revisionName")
	}
	return errs.Also(pt.Configuration.Validate().ViaField("configuration"))
}

func (rlt *RunLatestType) Validate() *apis.FieldError {
	return rlt.Configuration.Validate().ViaField("configuration")
}
