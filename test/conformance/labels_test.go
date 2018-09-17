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

package conformance

import (
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLabelsPropagation(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestLabelsPropagation")

	var imagePath = test.ImagePath("helloworld")

	var names test.ResourceNames
	names.Service = test.AppendRandomString("pizzaplanet-service", logger)

	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	logger.Info("The Service will be updated with the name of the Revision once it is created")
	var revisionName string
	err = test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	revisionName, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	logger.Info("Validate Labels on Revision Object")
	revision, err := clients.ServingClient.Revisions.Get(names.Revision, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Revision %s: %v", names.Revision, err)
	}

	if revConfigurationLabel, ok := revision.Labels["serving.knative.dev/configuration"]; ok {
		if revConfigurationLabel != names.Config {
			t.Fatalf("Expect confguration name in revision label %q but got %q ", names.Config, revConfigurationLabel)
		}
	} else {
		t.Fatalf("Failed to get configuration name from Revision label")
	}
	if revServiceLabel, ok := revision.Labels["serving.knative.dev/service"]; ok {
		if revServiceLabel != names.Service {
			t.Fatalf("Expect Service name in revision label %q but got %q ", names.Service, revServiceLabel)
		}
	} else {
		t.Fatalf("Failed to get Service name from Revision label")
	}

	logger.Info("Validate Labels on Configuration Object")
	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Configuration %s: %v", names.Config, err)
	}

	if configServiceLabel, ok := config.Labels["serving.knative.dev/service"]; ok {
		if configServiceLabel != names.Service {
			t.Fatalf("Expect Service name in configuration label %q but got %q ", names.Service, configServiceLabel)
		}
	} else {
		t.Fatalf("Failed to get service name from Configuration label")
	}

	if configRouteLabel, ok := config.Labels["serving.knative.dev/route"]; ok {
		if configRouteLabel != names.Route {
			t.Fatalf("Expect Route name in configuration label %q but got %q ", names.Route, configRouteLabel)
		}
	} else {
		t.Fatalf("Failed to get route name from Configuration label")
	}

	logger.Info("Validate Labels on Route Object")
	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}

	if routeServiceLabel, ok := route.Labels["serving.knative.dev/service"]; ok {
		if routeServiceLabel != names.Service {
			t.Fatalf("Expect Service name in Route label %q but got %q ", names.Service, routeServiceLabel)
		}
	} else {
		t.Fatalf("Failed to get service name from Route label")
	}

}
