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
	"fmt"
	"net/http"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnvVars(t *testing.T) {
	clients := Setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestEnvVars")

	var imagePath = test.ImagePath("envvars")

	var names test.ResourceNames
	names.Service = test.AppendRandomString("yashiki", logger)

	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	var revisionName string
	logger.Info("The Service will be updated with the name of the Revision once it is created")
	err = test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	envVarsExpectedOutput := fmt.Sprintf("Here are our env vars service:%s - configuration:%s - revision:%s", names.Service, names.Config, names.Revision)

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(pkgTest.MatchesBody(envVarsExpectedOutput), http.StatusNotFound),
		"EnvVarsServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, envVarsExpectedOutput, err)
	}
}
