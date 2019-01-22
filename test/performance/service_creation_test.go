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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/prometheus"
	"github.com/knative/test-infra/shared/testgrid"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

const (
	tCreateNServicesName = "TestCreate%dServices"
)

func testCreateServices(t *testing.T, tName string, noServices int) {
	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger(tName)

	fopt := []ServiceOption{
		// We set a small resource alloc so that we can pack more pods into the cluster.
		func(svc *v1alpha1.Service) {
			svc.Spec.RunLatest.Configuration.RevisionTemplate.Spec.Container.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
			}
		},
	}

	perfClients, err := Setup(context.Background(), logger, true)
	if err != nil {
		t.Fatalf("Cannot initialize performance client: %v", err)
	}
	clients := perfClients.E2EClients

	deployGrp, _ := errgroup.WithContext(context.Background())

	logger.Infof("Creating %d services", noServices)
	basename := test.AppendRandomString(fmt.Sprintf("perf-services-%03d-", noServices), logger)
	for i := 0; i < noServices; i++ {
		names := test.ResourceNames{
			Service: fmt.Sprintf("%s-%03d", basename, i),
			Image:   "helloworld",
		}
		test.CleanupOnInterrupt(func() { TearDown(perfClients, logger, names) }, logger)
		defer TearDown(perfClients, logger, names)

		deployGrp.Go(func() error {
			_, err := test.CreateLatestService(logger, clients, names, &test.Options{}, fopt...)
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
		t.Fatal("Error waiting for services to become ready")
	}

	// Wait for prometheus to scrape the data.
	prometheus.AllowPrometheusSync(logger)

	promAPI, err := prometheus.PromAPI()
	if err != nil {
		t.Fatal("Couldn't setup prometheus API")
	}

	var testCases []testgrid.TestCase
	for _, op := range []string{"avg", "max", "95q"} {
		metricName := fmt.Sprintf("%s_%s", op, reconciler.ServiceReadyLatencyN)
		var query string
		if op == "95q" {
			query = fmt.Sprintf("quantile(0.95, controller_%s{key=~\"%s/%s-.*\"})", reconciler.ServiceReadyLatencyN, test.ServingNamespace, basename)
		} else {
			query = fmt.Sprintf("%s(controller_%s{key=~\"%s/%s-.*\"})", op, reconciler.ServiceReadyLatencyN, test.ServingNamespace, basename)
		}
		val, err := prometheus.RunQuery(context.Background(), logger, promAPI, query)
		if err != nil {
			t.Fatalf("Failed prometheus query %q: %v", query, err)
		}
		tc := CreatePerfTestCase(float32(val), metricName, tName)
		testCases = append(testCases, tc)
	}

	if err = testgrid.CreateTestgridXML(testCases, tName); err != nil {
		t.Fatalf("Cannot create ouput XML: %v", err)
	}
}

func TestCreateServices(t *testing.T) {
	for _, s := range []int{1, 10} {
		n := fmt.Sprintf(tCreateNServicesName, s)
		t.Run(n, func(t *testing.T) {
			testCreateServices(t, n, s)
		})
	}
}
