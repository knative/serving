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

func (s *Service) Validate() *FieldError {
	return s.Spec.Validate().ViaField("spec")
}

func (ss *ServiceSpec) Validate() *FieldError {
	// We would do this semantic DeepEqual, but the spec is comprised
	// entirely of a oneof, the validation for which produces a clearer
	// error message.
	// if equality.Semantic.DeepEqual(ss, &ServiceSpec{}) {
	// 	return errMissingField(currentField)
	// }

	switch {
	case ss.RunLatest != nil && ss.Pinned != nil:
		return &FieldError{
			Message: "Expected exactly one, got both",
			Paths:   []string{"runLatest", "pinned"},
		}
	case ss.RunLatest != nil:
		return ss.RunLatest.Validate().ViaField("runLatest")
	case ss.Pinned != nil:
		return ss.Pinned.Validate().ViaField("pinned")
	default:
		return &FieldError{
			Message: "Expected exactly one, got neither",
			Paths:   []string{"runLatest", "pinned"},
		}
	}
}

func (pt *PinnedType) Validate() *FieldError {
	if pt.RevisionName == "" {
		return errMissingField("revisionName")
	}
	return pt.Configuration.Validate().ViaField("configuration")
}

func (rlt *RunLatestType) Validate() *FieldError {
	return rlt.Configuration.Validate().ViaField("configuration")
}
