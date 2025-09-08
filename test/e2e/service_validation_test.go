//go:build e2e
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
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	. "knative.dev/serving/pkg/testing/v1"
)

func withInvalidContainer() ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.Spec.PodSpec.Containers[0].Name = "&InvalidValue"
	}
}

func TestServiceDryRunValidationWithInvalidPodSpec(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Clean up on test failure or interrupt
	test.EnsureTearDown(t, clients, &names)

	// Setup Service
	service, err := v1test.CreateService(t, clients, names)
	if err != nil {
		t.Fatal("Create Service:", err)
	}

	_, err = PatchServiceWithDryRun(t, clients, service, withInvalidContainer())
	if err == nil {
		t.Fatal("Expected Service patch to fail")
	}
	if got, want := err.Error(), "validation callback failed"; !strings.Contains(got, want) {
		t.Errorf("Error = %q, want to contain = %q", got, want)
	}
}

// PatchServiceWithDryRun patches the existing service passed in with the applied mutations running in dryrun mode.
// Returns the latest service object
func PatchServiceWithDryRun(t testing.TB, clients *test.Clients, service *v1.Service, fopt ...ServiceOption) (svc *v1.Service, err error) {
	newSvc := service.DeepCopy()
	for _, opt := range fopt {
		opt(newSvc)
	}
	v1test.LogResourceObject(t, v1test.ResourceObjects{Service: newSvc})
	patchBytes, err := duck.CreateBytePatch(service, newSvc)
	if err != nil {
		return nil, err
	}
	return svc, reconciler.RetryTestErrors(func(int) (err error) {
		svc, err = clients.ServingClient.Services.Patch(context.Background(), service.ObjectMeta.Name, types.JSONPatchType, patchBytes, metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}})
		return err
	})
}
