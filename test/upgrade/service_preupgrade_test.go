// +build preupgrade

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

package upgrade

import (
	"testing"

	"github.com/knative/pkg/test/logging"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	_ "github.com/knative/serving/pkg/system/testing"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/e2e"
)

func TestRunLatestServicePreUpgrade(t *testing.T) {
	clients := e2e.Setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestRunLatestServicePreUpgrade")

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = image1

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	logger.Info("The Service will be updated with the name of the Revision once it is created")
	revisionName, err := waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}
	names.Revision = revisionName

	logger.Info("The Service will be updated with the domain of the Route once it is created")
	routeDomain, err := waitForServiceDomain(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new route: %v", names.Service, err)
	}

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "1", "What a spaceport!")
}
