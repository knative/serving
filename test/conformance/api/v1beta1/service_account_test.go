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

package v1beta1

import (
	"strings"
	"testing"

	. "knative.dev/serving/pkg/testing/v1beta1"
	"knative.dev/serving/test"
	v1b1test "knative.dev/serving/test/v1beta1"
)

const (
	invalidServiceAccountName = "foo@bar.baz"
)

func TestServiceAccountValidation(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	t.Logf("Creating a new Service %s", names.Service)
	service := v1b1test.Service(names, WithServiceAccountName(invalidServiceAccountName))
	v1b1test.LogResourceObject(t, v1b1test.ResourceObjects{Service: service})

	_, err := clients.ServingBetaClient.Services.Create(service)
	if err == nil {
		t.Fatal("Expected Service creation to fail")
	}
	if got, want := err.Error(), "serviceAccountName: spec.template.spec."+invalidServiceAccountName; !strings.Contains(got, want) {
		t.Errorf("Error = %q, want to contain = %q", got, want)
	}
}
