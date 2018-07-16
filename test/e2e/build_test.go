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
	"strings"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/test"
)

func TestBuildAndServe(t *testing.T) {
	clients := Setup(t)

	// Add test case specific name to its own logger.
	logger := test.Logger.Named("TestBuildAndServe")

	var imagePath string
	imagePath = strings.Join([]string{test.Flags.DockerRepo, "helloworld"}, "/")

	logger.Infof("Creating a new Route and Configuration with build")
	names := test.ResourceNames{
		Config: test.AppendRandomString(configName, logger),
		Route:  test.AppendRandomString(routeName, logger),
	}

	build := &buildv1alpha1.BuildSpec{
		Steps: []v1.Container{{
			Image: "ubuntu",
			Args:  []string{"echo", "built"},
		}},
	}

	config, err := clients.Configs.Create(test.ConfigurationWithBuild(test.Flags.Namespace, names, build, imagePath))
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration with build: %v", err)
	}
	if _, err := clients.Routes.Create(test.Route(test.Flags.Namespace, names)); err != nil {

		t.Fatalf("Failed to create Route and Configuration with build: %v", err)
	}

	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.Routes, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	err = test.WaitForEndpointState(clients.Kube, logger, test.Flags.ResolvableDomain, domain, test.MatchesBody(helloWorldExpectedOutput), "HelloWorldServesText")
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	// Get latest revision's Build, and check that the Build was successful.
	config, err = clients.Configs.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}
	rev, err := clients.Revisions.Get(config.Status.LatestReadyRevisionName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get latest Revision: %v", err)
	}
	buildName := rev.Spec.BuildName
	b, err := clients.Builds.Get(buildName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get build for latest revision: %v", err)
	}
	if cond := b.Status.GetCondition(buildv1alpha1.BuildSucceeded); cond == nil {
		t.Fatalf("Condition for build %q was nil", buildName)
	} else if cond.Status != v1.ConditionTrue {
		t.Fatalf("Build %q was not successful", buildName)
	}
}
