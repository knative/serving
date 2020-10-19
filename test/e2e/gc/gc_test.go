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

package gc

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func TestRevisionGC(t *testing.T) {
	t.Parallel()

	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Images:  []string{"helloworld"},
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	revision := resources.Revision
	if val := revision.Labels[serving.RoutingStateLabelKey]; val != "active" {
		t.Fatalf(`Got revision label %s=%q, want="active"`, serving.RoutingStateLabelKey, val)
	}

	t.Log("Updating the Service to use a different image.")
	image2 := pkgTest.ImagePath(test.PizzaPlanet2)
	if _, err := v1test.PatchService(t, clients, resources.Service, rtesting.WithServiceImage(image2)); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, image2, err)
	}

	t.Log("Service should reflect new revision created and ready in status.")
	names.Revision, err = v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatal("New image not reflected in Service:", err)
	}
	t.Log("Waiting for Service to transition to Ready.")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatal("Error waiting for the service to become ready for the latest revision:", err)
	}

	// Poll for a minute to see not_found on the original revision.
	var originalRevision *v1.Revision
	err = wait.PollImmediate(5*time.Second, time.Minute, func() (bool, error) {
		originalRevision, err = clients.ServingClient.Revisions.Get(context.Background(), revision.GetName(), metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if errors.Is(err, wait.ErrWaitTimeout) {
		t.Fatalf("Got revision %v, expected not_found", originalRevision)
	}
	if err != nil {
		t.Fatalf("Got %q, expected not_found", err)
	}
}
