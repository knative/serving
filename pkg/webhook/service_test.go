/*
Copyright 2018 Google LLC. All Rights Reserved.
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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestEmptySpec(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{},
	}
	err := ValidateService(testCtx)(nil, &s, &s)
	if err == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}
	if e, a := errInvalidRollouts, err; e != a {
		t.Errorf("Expected %s got %s", e, a)
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
	if err := ValidateService(testCtx)(nil, &s, &s); err != nil {
		t.Errorf("Expected success, but failed with: %s", err)
	}
}

func TestRunLatestWithMissingConfiguration(t *testing.T) {
	s := v1alpha1.Service{
		Spec: v1alpha1.ServiceSpec{
			RunLatest: &v1alpha1.RunLatestType{},
		},
	}
	err := ValidateService(testCtx)(nil, &s, &s)
	if err == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}

	if e, a := errServiceMissingField("spec.runLatest.configuration").Error(), err.Error(); e != a {
		t.Errorf("Expected %s got %s", e, a)
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

	if err := ValidateService(testCtx)(nil, &s, &s); err != nil {
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
	err := ValidateService(testCtx)(nil, &s, &s)
	if err == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}
	if e, a := errServiceMissingField("spec.pinned.revisionName").Error(), err.Error(); e != a {
		t.Errorf("Expected %s got %s", e, a)
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
	err := ValidateService(testCtx)(nil, &s, &s)
	if err == nil {
		t.Errorf("Expected failure, but succeeded with: %+v", s)
	}
	if e, a := errServiceMissingField("spec.pinned.configuration").Error(), err.Error(); e != a {
		t.Errorf("Expected %s got %s", e, a)
	}
}
