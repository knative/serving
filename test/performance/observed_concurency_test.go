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
	"fmt"
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/tools/testgrid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tName       = "TestOberservedConcurrency"
	perfLatency = "perf_latency"
	concurrency = 5
	duration    = "60s"
)

func createTestCase(val float32, percentile float64) testgrid.TestCase {
	tp := []testgrid.TestProperty{testgrid.TestProperty{Name: perfLatency, Value: val}}
	tc := testgrid.TestCase{
		ClassName:  tName,
		Name:       fmt.Sprintf("%s/p%f", tName, percentile),
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
	clients := Setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger(tName)

	var imagePath = test.ImagePath("observed-concrrency")
	names := test.ResourceNames{Service: test.AppendRandomString("observed-concurrency", logger)}

	defer TearDown(clients, names)
	test.CleanupOnInterrupt(func() { TearDown(clients, names) }, logger)

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
	endpoint, err := GetServiceEndpoint(clients)
	if err != nil {
		t.Fatalf("Cannot get service endpoint: %v", err)
	}

	resp, err := RunLoadTest(*endpoint, domain)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	var tc []testgrid.TestCase
	for _, p := range resp.DurationHistogram.Percentiles {
		tc = append(tc, createTestCase(float32(p.Value), p.Percentile))
	}

	if err = CreateTestgridXML(tc); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}
