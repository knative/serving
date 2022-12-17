//go:build e2e
// +build e2e

/*
Copyright 2021 The Knative Authors

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
	"testing"

	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// Test Revision Get and List operations.
//
//	This test doesn't validate the Data Plane, it is just to check the Control Plane resources and their APIs
func TestRevisionGetAndList(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Clean up on test failure or interrupt
	test.EnsureTearDown(t, clients, &names)

	// Setup initial Service
	if _, err := v1test.CreateServiceReady(t, clients, &names); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	revision, err := v1test.GetRevision(clients, names.Revision)
	if err != nil {
		t.Fatal("Getting revision failed")
	}

	revisions, err := v1test.GetRevisions(clients)
	if err != nil {
		t.Fatal("Getting revisions failed")
	}
	var revisionFound = false
	for _, revisionItem := range revisions.Items {
		t.Logf("Revision Returned: %s", revisionItem.Name)
		if revisionItem.Name == revision.Name {
			revisionFound = true
		}
	}

	if !revisionFound {
		t.Fatal("The Revision that was previously created was not found by listing all Revisions.")
	}

}
