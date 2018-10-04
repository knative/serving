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

package e2e

import (
	"net/http"
	"testing"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/pkg/apis/duck"
	testbuildv1alpha1 "github.com/knative/serving/test/apis/testing/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
)

var buildCondSet = duckv1alpha1.NewBatchConditionSet()

func TestBuildSpecAndServe(t *testing.T) {
	clients := Setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestBuildAndServe")

	imagePath := test.ImagePath("helloworld")

	logger.Infof("Creating a new Route and Configuration with build")
	names := test.ResourceNames{
		Config: test.AppendRandomString(configName, logger),
		Route:  test.AppendRandomString(routeName, logger),
	}

	build := &v1alpha1.RawExtension{
		BuildSpec: &buildv1alpha1.BuildSpec{
			Steps: []corev1.Container{{
				Image: "ubuntu",
				Args:  []string{"echo", "built"},
			}},
		},
	}

	if _, err := clients.ServingClient.Configs.Create(test.ConfigurationWithBuild(test.ServingNamespace, names, build, imagePath)); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}
	if _, err := clients.ServingClient.Routes.Create(test.Route(test.ServingNamespace, names)); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	endState := pkgTest.Retrying(pkgTest.MatchesBody(helloWorldExpectedOutput), http.StatusNotFound)
	if _, err := pkgTest.WaitForEndpointState(clients.KubeClient, logger, domain, endState, "HelloWorldServesText", test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	// Get Configuration's latest ready Revision's Build, and check that the Build was successful.
	logger.Infof("Revision is ready and serving, checking Build status.")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}
	rev, err := clients.ServingClient.Revisions.Get(config.Status.LatestReadyRevisionName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get latest Revision: %v", err)
	}
	buildName := rev.Spec.BuildRef.Name
	logger.Infof("Latest ready Revision is %q", rev.Name)
	logger.Infof("Revision's Build is %q", buildName)
	u, err := clients.Dynamic.Resource(schema.GroupVersionResource{
		Group:    buildv1alpha1.SchemeGroupVersion.Group,
		Version:  buildv1alpha1.SchemeGroupVersion.Version,
		Resource: "builds",
	}).Namespace(test.ServingNamespace).Get(buildName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get build for latest revision: %v", err)
	}
	b := &duckv1alpha1.KResource{}
	if err := duck.FromUnstructured(u, b); err != nil {
		t.Fatalf("Failed to cast to KResource: %+v", u)
	}
	if cond := buildCondSet.Manage(&b.Status).GetCondition(duckv1alpha1.ConditionSucceeded); cond == nil {
		t.Fatalf("Condition for build %q was nil", buildName)
	} else if cond.Status != corev1.ConditionTrue {
		t.Fatalf("Build %q was not successful", buildName)
	}
}

func TestBuildAndServe(t *testing.T) {
	clients := Setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestBuildAndServe")

	imagePath := test.ImagePath("helloworld")

	logger.Infof("Creating a new Route and Configuration with build")
	names := test.ResourceNames{
		Config: test.AppendRandomString(configName, logger),
		Route:  test.AppendRandomString(routeName, logger),
	}

	build := &v1alpha1.RawExtension{
		Object: &testbuildv1alpha1.Build{
			TypeMeta: metav1.TypeMeta{
				APIVersion: testbuildv1alpha1.SchemeGroupVersion.String(),
				Kind:       "Build",
			},
		},
	}

	if _, err := clients.ServingClient.Configs.Create(test.ConfigurationWithBuild(test.ServingNamespace, names, build, imagePath)); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}
	if _, err := clients.ServingClient.Routes.Create(test.Route(test.ServingNamespace, names)); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	endState := pkgTest.Retrying(pkgTest.MatchesBody(helloWorldExpectedOutput), http.StatusNotFound)
	if _, err := pkgTest.WaitForEndpointState(clients.KubeClient, logger, domain, endState, "HelloWorldServesText", test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	// Get Configuration's latest ready Revision's Build, and check that the Build was successful.
	logger.Infof("Revision is ready and serving, checking Build status.")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}
	rev, err := clients.ServingClient.Revisions.Get(config.Status.LatestReadyRevisionName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get latest Revision: %v", err)
	}
	buildName := rev.Spec.BuildRef.Name
	logger.Infof("Latest ready Revision is %q", rev.Name)
	logger.Infof("Revision's Build is %q", buildName)
	b, err := clients.BuildClient.TestBuilds.Get(buildName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get build for latest revision: %v", err)
	}
	if cond := b.Status.GetCondition(duckv1alpha1.ConditionSucceeded); cond == nil {
		t.Fatalf("Condition for build %q was nil", buildName)
	} else if cond.Status != corev1.ConditionTrue {
		t.Fatalf("Build %q was not successful", buildName)
	}
}

func TestBuildFailure(t *testing.T) {
	clients := Setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestBuildFailure")

	logger.Infof("Creating a new Configuration with failing build")
	names := test.ResourceNames{
		Config: test.AppendRandomString(configName, logger),
	}

	// Request a build that doesn't succeed.
	build := &v1alpha1.RawExtension{
		Object: &testbuildv1alpha1.Build{
			TypeMeta: metav1.TypeMeta{
				APIVersion: testbuildv1alpha1.SchemeGroupVersion.String(),
				Kind:       "Build",
			},
			Spec: testbuildv1alpha1.BuildSpec{
				Failure: &testbuildv1alpha1.FailureInfo{
					Message: "Test build has failed",
					Reason:  "InjectedFailure",
				},
			},
		},
	}

	imagePath := test.ImagePath("helloworld")
	config, err := clients.ServingClient.Configs.Create(test.ConfigurationWithBuild(test.ServingNamespace, names, build, imagePath))
	if err != nil {
		t.Fatalf("Failed to create Configuration with failing build: %v", err)
	}

	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	// Wait for the Config have a LatestCreatedRevisionName
	if err := test.WaitForConfigurationState(clients.ServingClient, names.Config, test.ConfigurationHasCreatedRevision, "ConfigurationHasCreatedRevision"); err != nil {
		t.Fatalf("The Configuration %q does not have a LatestCreatedRevisionName: %v", names.Config, err)
	}
	// Get Configuration's latest Revision's Build, and check that the Build failed.
	config, err = clients.ServingClient.Configs.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}
	// Wait for the Revision to notice its Build failed.
	if err := test.WaitForRevisionState(clients.ServingClient, config.Status.LatestCreatedRevisionName, test.IsRevisionBuildFailed, "RevisionIsBuildFailed"); err != nil {
		t.Fatalf("The Revision %q was not marked as having a failed build: %v", names.Revision, err)
	}
	logger.Infof("Revision %q is not ready because its build failed.", config.Status.LatestCreatedRevisionName)
	rev, err := clients.ServingClient.Revisions.Get(config.Status.LatestCreatedRevisionName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get latest Revision: %v", err)
	}
	buildName := rev.Spec.BuildRef.Name
	logger.Infof("Latest created Revision is %q", rev.Name)
	logger.Infof("Revision's Build is %q", buildName)
	b, err := clients.BuildClient.TestBuilds.Get(buildName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get build for latest revision: %v", err)
	}
	if cond := b.Status.GetCondition(duckv1alpha1.ConditionSucceeded); cond == nil {
		t.Fatalf("Condition for build %q was nil", buildName)
	} else if cond.Status != corev1.ConditionFalse {
		t.Fatalf("Build %q was not unsuccessful", buildName)
	}
}
