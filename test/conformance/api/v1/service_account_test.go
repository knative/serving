//go:build e2e
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
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	invalidServiceAccountName = "foo@bar.baz"
)

func TestServiceAccountValidation(t *testing.T) {
	if test.ServingFlags.DisableOptionalAPI {
		t.Skip("Service.spec.serviceAccountName is not required by Knative Serving API Specification")
	}

	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service", names.Service)
	service := v1test.Service(names, WithServiceAccountName(invalidServiceAccountName))
	v1test.LogResourceObject(t, v1test.ResourceObjects{Service: service})

	_, err := clients.ServingClient.Services.Create(context.Background(), service, metav1.CreateOptions{})
	if err == nil {
		t.Fatal("Expected Service creation to fail")
	}
	errorDetails := strings.Join(validation.IsDNS1123Subdomain("foo@bar.baz"), "\n")
	if got, want := err.Error(), invalidServiceAccountName+": spec.template.spec.serviceAccountName"+"\n"+errorDetails; !strings.Contains(got, want) {
		t.Errorf("Error = %q, want to contain = %q", got, want)
	}
}
