// +build performance

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

package performance

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/test-infra/shared/testgrid"
	"golang.org/x/sync/errgroup"

	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/knative/serving/test"
)

const (
	serviceName                      = "perftest-scalefromzero"
	ScaleFromZeroAvgTestGridProperty = "perf_ScaleFromZero_Average"
	helloWorldExpectedOutput         = "Hello World!"
)

type stats struct {
	avg time.Duration
}

func runScaleFromZero(clients *test.Clients, logger *logging.BaseLogger, ro *test.ResourceObjects) (time.Duration, error) {
	deploymentName := names.Deployment(ro.Revision)

	domain := ro.Route.Status.Domain
	logger.Info("Waiting for deployment to scale to zero.")
	if err := pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		test.DeploymentScaledToZeroFunc(),
		"DeploymentScaledToZero",
		test.ServingNamespace,
		2*time.Minute); err != nil {
		return 0, fmt.Errorf("Failed waiting for deployment to scale to zero: %v", err)
	}

	start := time.Now()
	logger.Info("Waiting for endpoint to serve request")
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(pkgTest.MatchesBody(helloWorldExpectedOutput), http.StatusNotFound),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain); err != nil {
		return 0, fmt.Errorf("The endpoint for Route %q at domain %q didn't serve the expected text %q: %v", ro.Route.Name, domain, helloWorldExpectedOutput, err)
	}

	logger.Info("Request completed")
	return time.Since(start), nil
}

func parallelScaleFromZero(t *testing.T, logger *logging.BaseLogger, count int) ([]time.Duration, error) {
	ctx := context.TODO()
	pc, err := Setup(ctx, logger, false)
	if err != nil {
		t.Fatalf("Failed to setup clients: %v", err)
	}

	testNames := make([]*test.ResourceNames, count)
	durations := make([]time.Duration, count)

	// Initialize our service names
	for i := 0; i < count; i++ {
		testNames[i] = &test.ResourceNames{
			Service: test.AppendRandomString(fmt.Sprintf("%s-%d", serviceName, i), logger),
			Image:   test.ImagePath("helloworld"),
		}
	}

	cleanupNames := func() {
		for i := 0; i < count; i++ {
			if testNames[i] != nil {
				TearDown(pc, logger, *testNames[i])
			}
		}
	}
	defer cleanupNames()
	test.CleanupOnInterrupt(cleanupNames, logger)

	g, _ := errgroup.WithContext(ctx)
	for i := 0; i < count; i++ {
		ndx := i
		g.Go(func() error {
			ro, err := test.CreateRunLatestServiceReady(logger, pc.E2EClients, testNames[ndx], &test.Options{})
			if err != nil {
				return fmt.Errorf("Failed to create Ready service: %v", err)
			}
			dur, err := runScaleFromZero(pc.E2EClients, logger, ro)
			if err == nil {
				durations[ndx] = dur
			}
			return err
		})
	}
	err = g.Wait()

	return durations, err
}

func getStats(durations []time.Duration) *stats {
	if len(durations) == 0 {
		return nil
	}
	var avg time.Duration

	for _, dur := range durations {
		avg += dur
	}
	avg = time.Duration(int64(avg) / int64(len(durations)))

	return &stats{
		avg: avg,
	}
}

func testGrid(s *stats, tName string) error {
	var tc []testgrid.TestCase
	val := float32(s.avg.Seconds() / 1000)
	tc = append(tc, CreatePerfTestCase(val, "Average", tName))
	return testgrid.CreateTestgridXML(tc, "TestPerformanceScaleFromZero")
}

func testScaleFromZero(t *testing.T, count int) {
	logger := logging.GetContextLogger(fmt.Sprintf("TestScaleFromZero%d", count))
	durs, err := parallelScaleFromZero(t, logger, count)
	if err != nil {
		t.Fatal(err)
	}
	stats := getStats(durs)
	logger.Infof("Average: %v", stats.avg)
	if err = testGrid(stats, strconv.Itoa(count)); err != nil {
		t.Fatalf("Creating testgrid output: %v", err)
	}
}

func TestScaleFromZero1(t *testing.T) {
	testScaleFromZero(t, 1)
}

func TestScaleFromZero5(t *testing.T) {
	testScaleFromZero(t, 5)
}

func TestScaleFromZero50(t *testing.T) {
	testScaleFromZero(t, 50)
}
