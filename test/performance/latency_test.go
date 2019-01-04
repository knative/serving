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
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/loadgenerator"
	"github.com/knative/test-infra/shared/testgrid"
)

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
		URL:            fmt.Sprintf("http://%s", *endpoint),
	}
	resp, err := opts.RunLoadTest(false)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}

	// Add latency metrics
	var tc []testgrid.TestCase
	for _, p := range resp.Result.DurationHistogram.Percentiles {
		val := float32(p.Value) * 1000
		name := fmt.Sprintf("p%d(sec)", int(p.Percentile))
		tc = append(tc, CreatePerfTestCase(val, name, "TestPerformanceLatency"))
	}

	if err = testgrid.CreateTestgridXML(tc, "TestPerformanceLatency"); err != nil {
		t.Fatalf("Cannot create output xml: %v", err)
	}
}
