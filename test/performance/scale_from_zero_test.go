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
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/test-infra/shared/testgrid"
	"golang.org/x/sync/errgroup"

	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/knative/serving/test"
	"github.com/knative/test-infra/shared/junit"
)

const (
	serviceName                      = "perftest-scalefromzero"
	ScaleFromZeroAvgTestGridProperty = "perf_ScaleFromZero_Average"
	helloWorldExpectedOutput         = "Hello World!"
	helloWorldImage                  = "helloworld"
	waitToServe                      = 10 * time.Minute
)

type stats struct {
	avg time.Duration
}

func runScaleFromZero(idx int, t *testing.T, clients *test.Clients, ro *test.ResourceObjects) (time.Duration, error) {
	t.Helper()
	deploymentName := names.Deployment(ro.Revision)

	domain := ro.Route.Status.Domain
	t.Logf("%02d: waiting for deployment to scale to zero.", idx)
	if err := pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		test.DeploymentScaledToZeroFunc,
		"DeploymentScaledToZero",
		test.ServingNamespace,
		3*time.Minute); err != nil {
		m := fmt.Sprintf("%02d: failed waiting for deployment to scale to zero: %v", idx, err)
		t.Log(m)
		return 0, errors.New(m)
	}

	start := time.Now()
	t.Logf("%0d: waiting for endpoint to serve request", idx)
	if _, err := pkgTest.WaitForEndpointStateWithTimeout(
		clients.KubeClient,
		t.Logf,
		domain,
		pkgTest.Retrying(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(helloWorldExpectedOutput)),
			http.StatusNotFound,            /* revision not created */
			http.StatusInternalServerError, /*revision not yet ready*/
		),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain, waitToServe); err != nil {
		m := fmt.Sprintf("%02d: the endpoint for Route %q at domain %q didn't serve the expected text %q: %v", idx, ro.Route.Name, domain, helloWorldExpectedOutput, err)
		t.Log(m)
		return 0, errors.New(m)
	}

	t.Logf("%02d: request completed", idx)
	return time.Since(start), nil
}

func parallelScaleFromZero(t *testing.T, count int) ([]time.Duration, error) {
	pc, err := Setup(context.Background(), t, false)
	if err != nil {
		return nil, fmt.Errorf("failed to setup clients: %v", err)
	}

	testNames := make([]*test.ResourceNames, count)
	durations := make([]time.Duration, count)

	// Initialize our service names.
	for i := 0; i < count; i++ {
		testNames[i] = &test.ResourceNames{
			Service: test.AppendRandomString(fmt.Sprintf("%s-%02d", serviceName, i)),
			// The crd.go helpers will convert to the actual image path.
			Image: helloWorldImage,
		}
	}

	cleanupNames := func() {
		for i := 0; i < count; i++ {
			TearDown(pc, *testNames[i])
		}
	}
	defer cleanupNames()
	test.CleanupOnInterrupt(cleanupNames)

	objs := make([]*test.ResourceObjects, count)
	begin := time.Now()
	defer func() {
		t.Logf("Total time for test: %v", time.Since(begin))
	}()
	g := errgroup.Group{}
	for i := 0; i < count; i++ {
		ndx := i
		g.Go(func() error {
			var err error
			if objs[ndx], err = test.CreateRunLatestServiceReady(t, pc.E2EClients, testNames[ndx], &test.Options{}); err != nil {
				return fmt.Errorf("%02d: failed to create Ready service: %v", ndx, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	t.Logf("Created all the services in %v", time.Since(begin))
	for i := 0; i < count; i++ {
		ndx := i
		g.Go(func() error {
			dur, err := runScaleFromZero(ndx, t, pc.E2EClients, objs[ndx])
			t.Logf("%02d: duration: %v, err: %v", ndx, dur, err)
			if err == nil {
				durations[ndx] = dur
			}
			return err
		})
	}
	return durations, g.Wait()
}

func getStats(durations []time.Duration) *stats {
	if len(durations) == 0 {
		return nil
	}
	var avg time.Duration

	for _, dur := range durations {
		avg += dur
	}
	return &stats{
		avg: time.Duration(int64(avg) / int64(len(durations))),
	}
}

func testScaleFromZero(t *testing.T, count int) {
	tName := fmt.Sprintf("TestScaleFromZero%02d", count)
	durs, err := parallelScaleFromZero(t, count)
	if err != nil {
		t.Fatal(err)
	}
	stats := getStats(durs)
	t.Logf("Average: %v", stats.avg)
	if err = testgrid.CreateXMLOutput([]junit.TestCase{
		CreatePerfTestCase(float32(stats.avg.Seconds()), "Average", tName)}, tName); err != nil {
		t.Fatalf("Error creating testgrid output: %v", err)
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
