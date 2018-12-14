// +build performance

/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// latency_test.go brings up a helloworld app and gets the latency metric

package performance

import (
	"context"
	"fmt"
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/testgrid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func waitForServiceLatestCreatedRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	err := test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	return revisionName, err
}

func TestPerformanceLatency(t *testing.T) {
	logger := logging.GetContextLogger("TestPerformanceLatency")

	perfClients, err := Setup(context.Background(), logger, true)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	names := test.ResourceNames{
		Service: test.AppendRandomString("helloworld", logger),
		Image:   "helloworld",
	}
	clients := perfClients.E2EClients

	defer TearDown(perfClients, logger, names)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, logger, names) }, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	logger.Info("The Service will be updated with the name of the Revision once it is created")
	names.Revision, err = waitForServiceLatestCreatedRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new revision: %v", names.Service, err)
	}

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Revision, err)
	}

	domain := route.Status.Domain
	endpoint, err := spoof.GetServiceEndpoint(clients.KubeClient.Kube)
	if err != nil {
		t.Fatalf("Cannot get service endpoint: %v", err)
	}

	url := fmt.Sprintf("http://%s", *endpoint)
	resp, err := RunLoadTest(duration, numThreads, concurrency, url, domain)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	// Add latency metrics
	var tc []testgrid.TestCase
	for _, p := range resp.DurationHistogram.Percentiles {
		tc = append(tc, CreatePerfTestCase(float32(p.Value), fmt.Sprintf("p%d", int(p.Percentile)), "TestPerformanceLatency"))
	}

	if err = testgrid.CreateTestgridXML(tc); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}
