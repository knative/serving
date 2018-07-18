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

package webhook

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	. "github.com/knative/serving/pkg/logging/testing"
	"github.com/mattbaird/jsonpatch"
)

func TestEmptySpec(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{},
	}
	got := Validate(TestContextWithLogger(t))(nil, &s, &s)
	if got == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}
	want := &v1alpha1.FieldError{
		Message: "Expected exactly one, got neither",
		Paths:   []string{"spec.runLatest", "spec.pinned"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestRunLatest(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			RunLatest: &v1alpha1.RunLatestType{
				Configuration: createConfiguration(1, "config").Spec,
			},
		},
	}
	if err := Validate(TestContextWithLogger(t))(nil, &s, &s); err != nil {
		t.Errorf("Expected success, but failed with: %s", err)
	}
}

func TestRunLatestWithMissingConfiguration(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			RunLatest: &v1alpha1.RunLatestType{},
		},
	}
	got := Validate(TestContextWithLogger(t))(nil, &s, &s)
	if got == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}
	want := &v1alpha1.FieldError{
		Message: "missing field(s)",
		Paths:   []string{"spec.runLatest.configuration"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestPinned(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			Pinned: &v1alpha1.PinnedType{
				RevisionName:  "revision",
				Configuration: createConfiguration(1, "config").Spec,
			},
		},
	}

	if err := Validate(TestContextWithLogger(t))(nil, &s, &s); err != nil {
		t.Errorf("Expected success, but failed with: %s", err)
	}
}

func TestPinnedFailsWithNoRevisionName(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			Pinned: &v1alpha1.PinnedType{
				Configuration: v1alpha1.ConfigurationSpec{},
			},
		},
	}
	got := Validate(TestContextWithLogger(t))(nil, &s, &s)
	if got == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}

	want := &v1alpha1.FieldError{
		Message: "missing field(s)",
		Paths:   []string{"spec.pinned.revisionName"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestPinnedFailsWithNoConfiguration(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			Pinned: &v1alpha1.PinnedType{
				RevisionName: "foo",
			},
		},
	}
	got := Validate(TestContextWithLogger(t))(nil, &s, &s)
	if got == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}

	want := &v1alpha1.FieldError{
		Message: "missing field(s)",
		Paths:   []string{"spec.pinned.configuration"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestPinnedSetsDefaults(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			Pinned: &v1alpha1.PinnedType{
				Configuration: createConfiguration(1, "config").Spec,
			},
		},
	}

	// Drop the ConcurrencyModel.
	s.Spec.Pinned.Configuration.RevisionTemplate.Spec.ConcurrencyModel = ""

	var patches []jsonpatch.JsonPatchOperation
	if err := SetDefaults(TestContextWithLogger(t))(&patches, &s); err != nil {
		t.Errorf("Expected success, but failed with: %s", err)
	}

	expected := []jsonpatch.JsonPatchOperation{{
		Operation: "add",
		Path:      "/spec/pinned/configuration/revisionTemplate/spec/concurrencyModel",
		Value:     "Multi",
	}}

	if diff := cmp.Diff(expected, patches); diff != "" {
		t.Errorf("SetDefaults (-want, +got) = %v", diff)
	}
}

func TestLatestSetsDefaults(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			RunLatest: &v1alpha1.RunLatestType{
				Configuration: createConfiguration(1, "config").Spec,
			},
		},
	}

	// Drop the ConcurrencyModel.
	s.Spec.RunLatest.Configuration.RevisionTemplate.Spec.ConcurrencyModel = ""

	var patches []jsonpatch.JsonPatchOperation
	if err := SetDefaults(TestContextWithLogger(t))(&patches, &s); err != nil {
		t.Errorf("Expected success, but failed with: %s", err)
	}

	expected := []jsonpatch.JsonPatchOperation{{
		Operation: "add",
		Path:      "/spec/runLatest/configuration/revisionTemplate/spec/concurrencyModel",
		Value:     "Multi",
	}}

	if diff := cmp.Diff(expected, patches); diff != "" {
		t.Errorf("SetDefaults (-want, +got) = %v", diff)
	}
}
