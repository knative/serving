// +build postupgrade

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
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/e2e"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRunLatestServicePostUpgrade(t *testing.T) {
	clients := e2e.Setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestRunLatestServicePostUpgrade")

	var names test.ResourceNames
	names.Service = serviceName

	logger.Infof("Getting service %q", names.Service)
	svc, err := clients.ServingClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Service: %v", err)
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	names.Revision = svc.Status.LatestCreatedRevisionName

	routeDomain := svc.Status.Domain

	logger.Info("Check that we can hit the old service and get the old response.")
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "1", "What a spaceport!")

	logger.Info("Updating the Service to use a different image")
	if _, err := test.PatchServiceImage(logger, clients, svc, test.ImagePath(image2)); err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, test.ImagePath(image2), err)
	}

	logger.Info("Since the Service was updated a new Revision will be created and the Service will be updated")
	revisionName, err := waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, image2, err)
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}
	assertServiceResourcesUpdated(t, logger, clients, names, routeDomain, "2", "Re-energize yourself with a slice of pepperoni!")
}
