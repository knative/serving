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

// service_test.go brings up an app and queries endpoints that perform
// knative service related operations.

package performance

import (
	"context"
	"fmt"
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/loadgenerator"
	"github.com/knative/test-infra/shared/testgrid"
)

func TestCreateService(t *testing.T) {
	logger := logging.GetContextLogger("TestCreateService")

	perfClients, err := Setup(context.Background(), logger, true)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}

	names := test.ResourceNames{
		Service: test.AppendRandomString("testcreateservice", logger),
		Image:   "knative-objects",
	}
	clients := perfClients.E2EClients

	defer TearDown(perfClients, logger, names)
	test.CleanupOnInterrupt(func() { TearDown(perfClients, logger, names) }, logger)

	logger.Info("Creating a new Service")
	objs, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	domain := objs.Route.Status.Domain
	endpoint, err := spoof.GetServiceEndpoint(clients.KubeClient.Kube)
	if err != nil {
		t.Fatalf("Cannot get service endpoint: %v", err)
	}

	opts := loadgenerator.GeneratorOptions{
		Duration:       duration,
		NumThreads:     1,
		NumConnections: 5,
		Domain:         domain,
		URL:            fmt.Sprintf("http://%s%s", *endpoint, test.KnativeObjectsCreateServicePath),
	}
	resp, err := opts.RunLoadTest(false)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	// Add latency metrics
	var tc []testgrid.TestCase
	for _, p := range resp.Result.DurationHistogram.Percentiles {
		tc = append(tc, CreatePerfTestCase(float32(p.Value), fmt.Sprintf("p%d", int(p.Percentile)), "TestCreateServicePerformance"))
	}

	if err = testgrid.CreateTestgridXML(tc, "TestCreateServicePerformance"); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}
