// +build e2e

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

package v1

import (
	"strings"
	"testing"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	invalidServiceAccountName = "foo@bar.baz"
)

func TestServiceValidationWithInvalidServiceAccount(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	t.Logf("Creating a new Service %s", names.Service)
	_, err := v1test.CreateService(t, clients, names, WithServiceAccountName(invalidServiceAccountName))

	if err == nil {
		t.Fatal("Expected Service creation to fail")
	}
	if got, want := err.Error(), "serviceAccountName: spec.template.spec."+invalidServiceAccountName; !strings.Contains(got, want) {
		t.Errorf("Error = %q, want to contain = %q", got, want)
	}
}

func TestServiceValidationWithInvalidPodSpec(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Clean up on test failure or interrupt
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	// Setup initial Service
	t.Logf("Creating a new Service %s", names.Service)
	_, err := v1test.CreateService(t, clients, names, func(svc *v1.Service) {
		svc.Spec.Template.Spec.PodSpec.Containers[0].Name = "&InvalidValue"
	})

	if err == nil {
		t.Fatal("Expected Service creation to fail")
	}
	if got, want := err.Error(), "PodSpec dry run failed"; !strings.Contains(got, want) {
		t.Errorf("Error = %q, want to contain = %q", got, want)
	}
}
