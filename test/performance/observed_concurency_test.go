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

package performance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/prometheus"
	"github.com/knative/test-infra/tools/testgrid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tName       = "TestObservedConcurrency"
	perfLatency = "perf_latency"
	concurrency = 5
	duration    = 1 * time.Minute
	numThreads  = 1
)

func createTestCase(val float32, name string) testgrid.TestCase {
	tp := []testgrid.TestProperty{{Name: perfLatency, Value: val}}
	tc := testgrid.TestCase{
		ClassName:  tName,
		Name:       fmt.Sprintf("%s/%s", tName, name),
		Properties: testgrid.TestProperties{Property: tp}}
	return tc
}

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

func TestObservedConcurrency(t *testing.T) {
	// add test case specific name to its own logger
	logger := logging.GetContextLogger(tName)

	perfClients, err := Setup(context.Background(), logger, true)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	var imagePath = test.ImagePath("observed-concurrency")
	names := test.ResourceNames{Service: test.AppendRandomString("observed-concurrency", logger)}
	clients := perfClients.E2EClients

	defer TearDown(perfClients, logger, names)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, logger, names) }, logger)

	logger.Info("Creating a new Service")
	svc, err := test.CreateLatestService(logger, clients, names, imagePath)
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

	url := fmt.Sprintf("http://%s/?timeout=1000", *endpoint)
	resp, err := RunLoadTest(duration, numThreads, concurrency, url, domain)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	// Wait for prometheus to scrape the data
	prometheus.AllowPrometheusSync(logger)

	promAPI, err := prometheus.PromAPI()
	if err != nil {
		logger.Errorf("Cannot setup prometheus API")
	}

	// Add latency metrics
	var tc []testgrid.TestCase
	for _, p := range resp.DurationHistogram.Percentiles {
		tc = append(tc, createTestCase(float32(p.Value), fmt.Sprintf("p%f", p.Percentile)))
	}

	// Add concurrency metrics
	metrics := []string{"autoscaler_observed_stable_concurrency", "autoscaler_observed_panic_concurrency", "autoscaler_target_concurrency_per_pod"}
	for _, metric := range metrics {
		val := prometheus.RunQuery(context.Background(), logger, promAPI, metric, names)
		tc = append(tc, createTestCase(float32(val), metric))
	}

	if err = testgrid.CreateTestgridXML(tc); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}
