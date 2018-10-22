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

	var errs *apis.FieldError
	set := []string{}

	if ss.RunLatest != nil {
		set = append(set, "runLatest")
		errs = errs.Also(ss.RunLatest.Validate().ViaField("runLatest"))
	}
	if ss.Release != nil {
		set = append(set, "release")
		errs = errs.Also(ss.Release.Validate().ViaField("release"))
	}
	if ss.Manual != nil {
		set = append(set, "manual")
		errs = errs.Also(ss.Manual.Validate().ViaField("manual"))
	}
	if ss.Pinned != nil {
		set = append(set, "pinned")
		errs = errs.Also(ss.Pinned.Validate().ViaField("pinned"))
	}

	if len(set) > 1 {
		errs = errs.Also(apis.ErrMultipleOneOf(set...))
	} else if len(set) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf("runLatest", "release", "manual", "pinned"))
	}
	return errs
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

func (m *ManualType) Validate() *apis.FieldError {
	return nil
}

func (rt *ReleaseType) Validate() *apis.FieldError {
	var errs *apis.FieldError

	numRevisions := len(rt.Revisions)
	if numRevisions < 1 || numRevisions > 2 {
		errs = errs.Also(apis.ErrInvalidValue(fmt.Sprintf("%v", numRevisions), "revisions.length"))
	}

	if numRevisions < 2 && rt.RolloutPercent != 0 {
		errs = errs.Also(apis.ErrInvalidValue(fmt.Sprintf("%v", rt.RolloutPercent), "rolloutPercent"))
	}

	if rt.RolloutPercent < 0 || rt.RolloutPercent > 99 {
		errs = errs.Also(apis.ErrInvalidValue(fmt.Sprintf("%v", rt.RolloutPercent), "rolloutPercent"))
	}

	return errs.Also(rt.Configuration.Validate().ViaField("configuration"))
}
