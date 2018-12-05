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

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/prometheus"
	"github.com/knative/test-infra/shared/testgrid"
	"golang.org/x/sync/errgroup"
)

const (
	tCreateServiceN    = "TestCreateService"
	tCreate20ServicesN = "TestCreate20Services"
)

func testCreateServices(t *testing.T, tName string, noServices int) {
	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger(tName)

	perfClients, err := Setup(context.Background(), logger, true)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}
	clients := perfClients.E2EClients

	deployGrp, _ := errgroup.WithContext(context.Background())

	logger.Infof("Creating %v services", noServices)
	basename := test.AppendRandomString(fmt.Sprintf("perf-services-%03d-", noServices), logger)
	for i := 0; i < noServices; i++ {
		names := test.ResourceNames{
			Service: fmt.Sprintf("%s-%03d", basename, i),
			Image:   "helloworld",
		}
		test.CleanupOnInterrupt(func() { TearDown(perfClients, logger, names) }, logger)
		defer TearDown(perfClients, logger, names)

		deployGrp.Go(func() error {
			_, err := test.CreateLatestServiceWithResources(logger, clients, names)
			if err != nil {
				t.Fatalf("Failed to create Service: %v", err)
			}
			logger.Infof("Wait for %s to become ready", names.Service)
			if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
				return err
			}
			return nil
		})
	}

	if err := deployGrp.Wait(); err != nil {
		t.Fatalf("Error waiting for services to become ready")
	}

	// Wait for prometheus to scrape the data.
	prometheus.AllowPrometheusSync(logger)

	promAPI, err := prometheus.PromAPI()
	if err != nil {
		logger.Error("Cannot setup prometheus API")
	}

	query := fmt.Sprintf("sum(controller_%s{key=~\"%s/%s-.*\"})", reconciler.ServiceReadyLatencyN, test.ServingNamespace, basename)
	logger.Infof("Query: %v", query)
	val, err := prometheus.RunQuery(context.Background(), logger, promAPI, query)
	if err != nil {
		logger.Errorf("Error querying metric %s: %v", query, err)
	} else {
		tc := CreatePerfTestCase(float32(val)/float32(noServices), reconciler.ServiceReadyLatencyN, tName)

		if err = testgrid.CreateTestgridXML([]testgrid.TestCase{tc}, tName); err != nil {
			t.Fatalf("Cannot create ouput XML: %v", err)
		}
	}
}

func TestCreateService(t *testing.T) {
	testCreateServices(t, tCreateServiceN, 1)
}

func TestCreate20Services(t *testing.T) {
	testCreateServices(t, tCreate20ServicesN, 20)
}
