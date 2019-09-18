// +build e2e

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

package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

// TestRunLatestService tests both Creation and Update paths of a runLatest service. The test performs a series of Update/Validate steps to ensure that
// the service transitions as expected during each step.
// Currently the test performs the following updates:
// 1. Update Container Image
// 2. Update Metadata
//    a. Update Labels
//    b. Update Annotations
func TestRunLatestService(t *testing.T) {
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
	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation

	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, test.PizzaPlanetText1); err != nil {
		t.Error(err)
	}

	if err = validateLabelsPropagation(t, *objects, names); err != nil {
		t.Error(err)
	}

	if err := validateAnnotations(objects); err != nil {
		t.Errorf("Service annotations are incorrect: %v", err)
	}

	// We start a background prober to test if Route is always healthy even during Route update.
	prober := test.RunRouteProber(t.Logf, clients, names.Domain)
	defer test.AssertProberDefault(t, prober)

	// Update Container Image
	t.Log("Updating the Service to use a different image.")
	names.Image = test.PizzaPlanet2
	image2 := pkgTest.ImagePath(names.Image)
	if _, err := v1a1test.PatchServiceImage(t, clients, objects.Service, image2); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, image2, err)
	}

	t.Log("Service should reflect new revision created and ready in status.")
	names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("New image not reflected in Service: %v", err)
	}

	t.Log("Waiting for Service to transition to Ready.")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}

	// Validate State after Image Update
	if err = validateRunLatestControlPlane(t, clients, names, "2"); err != nil {
		t.Error(err)
	}
	if err = validateRunLatestDataPlane(t, clients, names, test.PizzaPlanetText2); err != nil {
		t.Error(err)
	}

	// Update Metadata (Labels)
	t.Logf("Updating labels of the RevisionTemplateSpec for service %s.", names.Service)
	metadata := metav1.ObjectMeta{
		Labels: map[string]string{
			"labelX": "abc",
			"labelY": "def",
		},
	}
	if objects.Service, err = v1a1test.PatchServiceTemplateMetadata(t, clients, objects.Service, metadata); err != nil {
		t.Fatalf("Service %s was not updated with labels in its RevisionTemplateSpec: %v", names.Service, err)
	}

	t.Log("Waiting for the new revision to appear as LatestRevision.")
	if names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names); err != nil {
		t.Fatalf("The Service %s was not updated with new revision %s after updating labels in its RevisionTemplateSpec: %v", names.Service, names.Revision, err)
	}

	// Update Metadata (Annotations)
	t.Logf("Updating annotations of RevisionTemplateSpec for service %s", names.Service)
	metadata = metav1.ObjectMeta{
		Annotations: map[string]string{
			"annotationA": "123",
			"annotationB": "456",
		},
	}
	if objects.Service, err = v1a1test.PatchServiceTemplateMetadata(t, clients, objects.Service, metadata); err != nil {
		t.Fatalf("Service %s was not updated with annotation in its RevisionTemplateSpec: %v", names.Service, err)
	}

	t.Log("Waiting for the new revision to appear as LatestRevision.")
	names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("The new revision has not become ready in Service: %v", err)
	}

	t.Log("Waiting for Service to transition to Ready.")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}

	// Validate the Service shape.
	if err = validateRunLatestControlPlane(t, clients, names, "4"); err != nil {
		t.Error(err)
	}
	if err = validateRunLatestDataPlane(t, clients, names, test.PizzaPlanetText2); err != nil {
		t.Error(err)
	}
}

func waitForDesiredTrafficShape(t *testing.T, sName string, want map[string]v1alpha1.TrafficTarget, clients *test.Clients) error {
	return v1a1test.WaitForServiceState(
		clients.ServingAlphaClient, sName, func(s *v1alpha1.Service) (bool, error) {
			// IsServiceReady never returns an error.
			if ok, _ := v1a1test.IsServiceReady(s); !ok {
				return false, nil
			}
			// Match the traffic shape.
			got := map[string]v1alpha1.TrafficTarget{}
			for _, tt := range s.Status.Traffic {
				got[tt.Tag] = tt
			}
			ignoreURLs := cmpopts.IgnoreFields(v1alpha1.TrafficTarget{},
				"TrafficTarget.URL", "DeprecatedName")
			if !cmp.Equal(got, want, ignoreURLs) {
				t.Logf("For service %s traffic shape mismatch: (-got, +want) %s",
					sName, cmp.Diff(got, want, ignoreURLs))
				return false, nil
			}
			return true, nil
		}, "Verify Service Traffic Shape",
	)
}

func TestRunLatestServiceBYOName(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Clean up on test failure or interrupt
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	revName := names.Service + "-byoname"

	// Setup initial Service
	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, func(svc *v1alpha1.Service) {
		svc.Spec.ConfigurationSpec.GetTemplate().Name = revName
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}
	if got, want := names.Revision, revName; got != want {
		t.Errorf("CreateRunLatestServiceReady() = %s, wanted %s", got, want)
	}

	// Validate State after Creation

	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, test.PizzaPlanetText1); err != nil {
		t.Error(err)
	}

	if err = validateLabelsPropagation(t, *objects, names); err != nil {
		t.Error(err)
	}

	if err := validateAnnotations(objects); err != nil {
		t.Errorf("Service annotations are incorrect: %v", err)
	}

	// We start a background prober to test if Route is always healthy even during Route update.
	prober := test.RunRouteProber(t.Logf, clients, names.Domain)
	defer test.AssertProberDefault(t, prober)

	// Update Container Image
	t.Log("Updating the Service to use a different image.")
	names.Image = test.PizzaPlanet2
	image2 := pkgTest.ImagePath(names.Image)
	if _, err := v1a1test.PatchServiceImage(t, clients, objects.Service, image2); err == nil {
		t.Fatalf("Patch update for Service %s didn't fail.", names.Service)
	}
}

// TestReleaseService creates a Service with a variety of "release"-like traffic shapes.
// Currently tests for the following combinations:
// 1. One Revision Specified, current == latest
// 2. One Revision Specified, current != latest
// 3. Two Revisions Specified, 50% rollout,  candidate == latest
// 4. Two Revisions Specified, 50% rollout, candidate != latest
// 5. Two Revisions Specified, 50% rollout, candidate != latest, candidate is configurationName.
func TestReleaseService(t *testing.T) {
	t.Parallel()
	// Create Initial Service
	clients := test.Setup(t)
	releaseImagePath2 := pkgTest.ImagePath(test.PizzaPlanet2)
	releaseImagePath3 := pkgTest.ImagePath(test.HelloWorld)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	// Expected Text for different revisions.
	const (
		expectedFirstRev  = test.PizzaPlanetText1
		expectedSecondRev = test.PizzaPlanetText2
		expectedThirdRev  = test.HelloWorldText
	)

	// Setup initial Service
	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	t.Log("Validating service shape.")
	if err := validateReleaseServiceShape(objects); err != nil {
		t.Fatalf("Release shape is incorrect: %v", err)
	}
	if err := validateAnnotations(objects); err != nil {
		t.Errorf("Service annotations are incorrect: %v", err)
	}
	firstRevision := names.Revision

	// 1. One Revision Specified, current == latest.
	t.Log("1. Updating Service to ReleaseType using lastCreatedRevision")
	objects.Service, err = v1a1test.UpdateServiceRouteSpec(t, clients, names, v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				Tag:          "current",
				RevisionName: firstRevision,
				Percent:      ptr.Int64(100),
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:     "latest",
				Percent: nil,
			},
		}},
	})
	if err != nil {
		t.Fatalf("Failed to update Service: %v", err)
	}

	desiredTrafficShape := map[string]v1alpha1.TrafficTarget{
		"current": {
			TrafficTarget: v1.TrafficTarget{
				Tag:            "current",
				RevisionName:   objects.Config.Status.LatestReadyRevisionName,
				Percent:        ptr.Int64(100),
				LatestRevision: ptr.Bool(false),
			},
		},
		"latest": {
			TrafficTarget: v1.TrafficTarget{
				Tag:            "latest",
				RevisionName:   objects.Config.Status.LatestReadyRevisionName,
				LatestRevision: ptr.Bool(true),
			},
		},
	}
	t.Log("Waiting for Service to become ready with the new shape.")
	if err := waitForDesiredTrafficShape(t, names.Service, desiredTrafficShape, clients); err != nil {
		t.Fatal("Service never obtained expected shape")
	}

	t.Log("Service traffic should go to the first revision and be available on two names traffic targets: 'current' and 'latest'")
	if err := validateDomains(t, clients,
		names.Domain,
		[]string{expectedFirstRev},
		[]string{"latest", "current"},
		[]string{expectedFirstRev, expectedFirstRev}); err != nil {
		t.Fatal(err)
	}

	// 2. One Revision Specified, current != latest.
	t.Log("2. Updating the Service Spec with a new image")
	if objects.Service, err = v1a1test.PatchServiceImage(t, clients, objects.Service, releaseImagePath2); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, releaseImagePath2, err)
	}

	t.Log("Since the Service was updated a new Revision will be created")
	if names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names); err != nil {
		t.Fatalf("The Service %s was not updated with new revision %s: %v", names.Service, names.Revision, err)
	}
	secondRevision := names.Revision

	// Also verify traffic is in the correct shape.
	desiredTrafficShape["latest"] = v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			Tag:            "latest",
			RevisionName:   secondRevision,
			LatestRevision: ptr.Bool(true),
		},
	}
	t.Log("Waiting for Service to become ready with the new shape.")
	if err := waitForDesiredTrafficShape(t, names.Service, desiredTrafficShape, clients); err != nil {
		t.Fatal("Service never obtained expected shape")
	}

	t.Log("Since the Service is using release the Route will not be updated, but new revision will be available at 'latest'")
	if err := validateDomains(t, clients,
		names.Domain,
		[]string{expectedFirstRev},
		[]string{"latest", "current"},
		[]string{expectedSecondRev, expectedFirstRev}); err != nil {
		t.Fatal(err)
	}

	// 3. Two Revisions Specified, 50% rollout, candidate == latest.
	t.Log("3. Updating Service to split traffic between two revisions using Release mode")
	objects.Service, err = v1a1test.UpdateServiceRouteSpec(t, clients, names, v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				Tag:          "current",
				RevisionName: firstRevision,
				Percent:      ptr.Int64(50),
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:          "candidate",
				RevisionName: secondRevision,
				Percent:      ptr.Int64(50),
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:     "latest",
				Percent: nil,
			},
		}},
	})
	if err != nil {
		t.Fatalf("Failed to update Service: %v", err)
	}

	desiredTrafficShape = map[string]v1alpha1.TrafficTarget{
		"current": {
			TrafficTarget: v1.TrafficTarget{
				Tag:            "current",
				RevisionName:   firstRevision,
				Percent:        ptr.Int64(50),
				LatestRevision: ptr.Bool(false),
			},
		},
		"candidate": {
			TrafficTarget: v1.TrafficTarget{
				Tag:            "candidate",
				RevisionName:   secondRevision,
				Percent:        ptr.Int64(50),
				LatestRevision: ptr.Bool(false),
			},
		},
		"latest": {
			TrafficTarget: v1.TrafficTarget{
				Tag:            "latest",
				RevisionName:   secondRevision,
				LatestRevision: ptr.Bool(true),
			},
		},
	}
	t.Log("Waiting for Service to become ready with the new shape.")
	if err := waitForDesiredTrafficShape(t, names.Service, desiredTrafficShape, clients); err != nil {
		t.Fatal("Service never obtained expected shape")
	}

	t.Log("Traffic should be split between the two revisions and available on three named traffic targets, 'current', 'candidate', and 'latest'")
	if err := validateDomains(t, clients,
		names.Domain,
		[]string{expectedFirstRev, expectedSecondRev},
		[]string{"candidate", "latest", "current"},
		[]string{expectedSecondRev, expectedSecondRev, expectedFirstRev}); err != nil {
		t.Fatal(err)
	}

	// 4. Two Revisions Specified, 50% rollout, candidate != latest.
	t.Log("4. Updating the Service Spec with a new image")
	if objects.Service, err = v1a1test.PatchServiceImage(t, clients, objects.Service, releaseImagePath3); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, releaseImagePath3, err)
	}
	t.Log("Since the Service was updated a new Revision will be created")
	if names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names); err != nil {
		t.Fatalf("The Service %s was not updated with new revision %s: %v", names.Service, names.Revision, err)
	}
	thirdRevision := names.Revision

	desiredTrafficShape["latest"] = v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			Tag:            "latest",
			RevisionName:   thirdRevision,
			LatestRevision: ptr.Bool(true),
		},
	}
	t.Log("Waiting for Service to become ready with the new shape.")
	if err := waitForDesiredTrafficShape(t, names.Service, desiredTrafficShape, clients); err != nil {
		t.Fatal("Service never obtained expected shape")
	}

	t.Log("Traffic should remain between the two images, and the new revision should be available on the named traffic target 'latest'")
	if err := validateDomains(t, clients,
		names.Domain,
		[]string{expectedFirstRev, expectedSecondRev},
		[]string{"latest", "candidate", "current"},
		[]string{expectedThirdRev, expectedSecondRev, expectedFirstRev}); err != nil {
		t.Fatal(err)
	}

	// Now update the service to use `@latest` as candidate.
	t.Log("5. Updating Service to split traffic between two `current` and `@latest`")

	objects.Service, err = v1a1test.UpdateServiceRouteSpec(t, clients, names, v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				Tag:          "current",
				RevisionName: firstRevision,
				Percent:      ptr.Int64(50),
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:     "candidate",
				Percent: ptr.Int64(50),
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:     "latest",
				Percent: nil,
			},
		}},
	})
	if err != nil {
		t.Fatalf("Failed to update Service: %v", err)
	}

	// Verify in the end it's still the case.
	if err := validateAnnotations(objects); err != nil {
		t.Errorf("Service annotations are incorrect: %v", err)
	}

	// `candidate` now points to the latest.
	desiredTrafficShape["candidate"] = v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			Tag:            "candidate",
			RevisionName:   thirdRevision,
			Percent:        ptr.Int64(50),
			LatestRevision: ptr.Bool(true),
		},
	}
	t.Log("Waiting for Service to become ready with the new shape.")
	if err := waitForDesiredTrafficShape(t, names.Service, desiredTrafficShape, clients); err != nil {
		t.Fatal("Service never obtained expected shape")
	}

	if err := validateDomains(t, clients,
		names.Domain,
		[]string{expectedFirstRev, expectedThirdRev},
		[]string{"latest", "candidate", "current"},
		[]string{expectedThirdRev, expectedThirdRev, expectedFirstRev}); err != nil {
		t.Fatal(err)
	}
}

func TestAnnotationPropagation(t *testing.T) {
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
	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation

	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err := validateAnnotations(objects); err != nil {
		t.Errorf("Annotations are incorrect: %v", err)
	}

	desiredSvc := objects.Service.DeepCopy()
	desiredSvc.Annotations["juicy"] = "jamba"
	if objects.Service, err = v1a1test.PatchService(t, clients, objects.Service, desiredSvc); err != nil {
		t.Fatalf("Service %s was not updated with new annotation: %v", names.Service, err)
	}
	// Updating metadata does not trigger revision or generation
	// change, so let's generate a change that we can watch.
	t.Log("Updating the Service to use a different image.")
	names.Image = test.PizzaPlanet2
	image2 := pkgTest.ImagePath(names.Image)
	if _, err := v1a1test.PatchServiceImage(t, clients, objects.Service, image2); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, image2, err)
	}

	t.Log("Service should reflect new revision created and ready in status.")
	names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("New image not reflected in Service: %v", err)
	}

	t.Log("Waiting for Service to transition to Ready.")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}
	objects, err = v1a1test.GetResourceObjects(clients, names)
	if err != nil {
		t.Errorf("Error getting objects: %v", err)
	}

	// Now we can validate the annotations.
	if err := validateAnnotations(objects, "juicy"); err != nil {
		t.Errorf("Annotations are incorrect: %v", err)
	}

	// Test annotation deletion.
	desiredSvc = objects.Service.DeepCopy()
	delete(desiredSvc.Annotations, "juicy")
	if objects.Service, err = v1a1test.PatchService(t, clients, objects.Service, desiredSvc); err != nil {
		t.Fatalf("Service %s was not updated with deleted annotation: %v", names.Service, err)
	}
	// Updating metadata does not trigger revision or generation
	// change, so let's generate a change that we can watch.
	t.Log("Updating the Service to use a different image.")
	names.Image = test.HelloWorld
	image3 := pkgTest.ImagePath(test.HelloWorld)
	if _, err := v1a1test.PatchServiceImage(t, clients, objects.Service, image3); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, image3, err)
	}

	t.Log("Service should reflect new revision created and ready in status.")
	names.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("New image not reflected in Service: %v", err)
	}

	t.Log("Waiting for Service to transition to Ready.")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("Error waiting for the service to become ready for the latest revision: %v", err)
	}
	objects, err = v1a1test.GetResourceObjects(clients, names)
	if err != nil {
		t.Errorf("Error getting objects: %v", err)
	}

	// Now we can validate the annotations.
	if err := validateAnnotations(objects); err != nil {
		t.Errorf("Annotations are incorrect: %v", err)
	}
	if _, ok := objects.Config.Annotations["juicy"]; ok {
		t.Error("Config still has `juicy` annotation")
	}
	if _, ok := objects.Route.Annotations["juicy"]; ok {
		t.Error("Route still has `juicy` annotation")
	}
}
