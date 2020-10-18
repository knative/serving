// +build e2e

/*
Copyright 2020 The Knative Authors

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

package e2e

import (
	"strings"
	"testing"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/pkg/webhook"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func withInvalidContainer() ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.Spec.PodSpec.Containers[0].Name = "&InvalidValue"
	}
}

func TestServiceValidationWithInvalidPodSpec(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Images:  []string{test.PizzaPlanet1},
	}

	// Clean up on test failure or interrupt
	test.EnsureTearDown(t, clients, &names)

	// Setup Service
	t.Log("Creating a new Service", names.Service)
	service, err := v1test.CreateService(t, clients, names,
		WithServiceAnnotation(webhook.PodSpecDryRunAnnotation, string(webhook.DryRunStrict)))
	if err != nil {
		t.Fatal("Create Service:", err)
	}

	_, err = v1test.PatchService(t, clients, service, withInvalidContainer())
	if err == nil {
		t.Fatal("Expected Service patch to fail")
	}
	if got, want := err.Error(), "validation callback failed"; !strings.Contains(got, want) {
		t.Errorf("Error = %q, want to contain = %q", got, want)
	}
}
