package v1

import (
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
	"testing"
)

// Test Revision Get and List operations.
//   This test doesn't validate the Data Plane, it is just to check the Control Plane resources and their APIs
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

	// Validate State after Creation
	if err := validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
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
