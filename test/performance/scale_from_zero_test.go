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
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"
	ktest "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1a1test "knative.dev/serving/test/v1alpha1"
	"knative.dev/test-infra/shared/junit"
	perf "knative.dev/test-infra/shared/performance"
	"knative.dev/test-infra/shared/testgrid"

	pkgTest "knative.dev/pkg/test"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	serviceName              = "perftest-scalefromzero"
	helloWorldExpectedOutput = "Hello World!"
	helloWorldImage          = "helloworld"
	waitToServe              = 10 * time.Minute
)

type stats struct {
	avg time.Duration
	min time.Duration
	max time.Duration
}

func runScaleFromZero(idx int, t *testing.T, clients *test.Clients, ro *v1a1test.ResourceObjects) (time.Duration, error) {
	t.Helper()
	deploymentName := names.Deployment(ro.Revision)

	url := ro.Route.Status.URL.URL()
	t.Logf("%02d: waiting for deployment to scale to zero.", idx)
	if err := e2e.WaitForScaleToZero(t, deploymentName, clients); err != nil {
		m := fmt.Sprintf("%02d: failed waiting for deployment to scale to zero: %v", idx, err)
		t.Log(m)
		return 0, errors.New(m)
	}

	start := time.Now()
	t.Logf("%02d: waiting for endpoint to serve request", idx)
	if _, err := pkgTest.WaitForEndpointStateWithTimeout(
		clients.KubeClient,
		t.Logf,
		url,
		pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(helloWorldExpectedOutput)),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain, waitToServe); err != nil {
		m := fmt.Sprintf("%02d: the endpoint for Route %q at %q didn't serve the expected text %q: %v", idx, ro.Route.Name, url, helloWorldExpectedOutput, err)
		t.Log(m)
		return 0, errors.New(m)
	}

	t.Logf("%02d: request completed", idx)

	return time.Since(start), nil
}

func createServices(t *testing.T, pc *Client, count int) ([]*v1a1test.ResourceObjects, func(), error) {
	testNames := make([]*test.ResourceNames, count)

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
			TearDown(pc, *testNames[i], t.Logf)
		}
	}
	test.CleanupOnInterrupt(cleanupNames)

	objs := make([]*v1a1test.ResourceObjects, count)
	begin := time.Now()
	defer func() {
		t.Logf("Total time for test: %v", time.Since(begin))
	}()
	sos := []ktest.ServiceOption{
		// We set a small resource alloc so that we can pack more pods into the cluster.
		ktest.WithResourceRequirements(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("20Mi"),
			},
		}),
		ktest.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey: "7s",
		}),
	}
	g := errgroup.Group{}
	for i := 0; i < count; i++ {
		ndx := i
		g.Go(func() error {
			var err error
			if objs[ndx], _, err = v1a1test.CreateRunLatestServiceReady(t, pc.E2EClients, testNames[ndx],
				false, /* https TODO(taragu) turn this on after helloworld test running with https */
				sos...); err != nil {
				return fmt.Errorf("%02d: failed to create Ready service: %v", ndx, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	t.Logf("Created all the services in %v", time.Since(begin))
	return objs, cleanupNames, nil
}

func parallelScaleFromZero(t *testing.T, pc *Client, objs []*v1a1test.ResourceObjects, count int) ([]time.Duration, error) {
	g := errgroup.Group{}
	durations := make([]time.Duration, count)
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

func getRunStats(durations []time.Duration) *stats {
	if len(durations) == 0 {
		return nil
	}
	min := durations[0]
	max := durations[0]
	avg := durations[0]

	for _, dur := range durations[1:] {
		if dur < min {
			min = dur
		} else if dur > max {
			max = dur
		}
		avg += dur
	}
	return &stats{
		avg: time.Duration(int64(avg) / int64(len(durations))),
		min: min,
		max: max,
	}
}

func getMultiRunStats(runStats []*stats) *stats {
	min := runStats[0].min
	max := runStats[0].max
	avg := runStats[0].avg

	for _, stat := range runStats[1:] {
		if stat.min < min {
			min = stat.min
		} else if stat.max > max {
			max = stat.max
		}
		avg += stat.avg
	}
	return &stats{
		avg: time.Duration(int64(avg) / int64(len(runStats))),
		min: min,
		max: max,
	}
}

func testScaleFromZero(t *testing.T, count, numRuns int) {
	pc, err := Setup(t)
	if err != nil {
		t.Fatalf("Failed to setup clients: %v", err)
	}
	// Create the services once.
	objs, cleanup, err := createServices(t, pc, count)
	if err != nil {
		t.Fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	runStats := make([]*stats, numRuns)
	tName := fmt.Sprintf("TestScaleFromZero%02d", count)
	for i := 0; i < numRuns; i++ {
		durs, err := parallelScaleFromZero(t, pc, objs, count)
		if err != nil {
			t.Fatalf("Run %d: %v", i+1, err)
		}
		runStats[i] = getRunStats(durs)
		t.Logf("Run %d: Average: %v", i+1, runStats[i].avg)
	}

	stats := getMultiRunStats(runStats)

	if err := testgrid.CreateXMLOutput([]junit.TestCase{
		perf.CreatePerfTestCase(float32(stats.avg.Seconds()), "Average", tName),
		perf.CreatePerfTestCase(float32(stats.min.Seconds()), "Min", tName),
		perf.CreatePerfTestCase(float32(stats.max.Seconds()), "Max", tName)}, tName); err != nil {
		t.Fatalf("Error creating testgrid output: %v", err)
	}
}

func TestScaleFromZero1(t *testing.T) {
	testScaleFromZero(t, 1 /* parallelism */, 5 /* runs */)
}

func TestScaleFromZero5(t *testing.T) {
	testScaleFromZero(t, 5 /* parallelism */, 5 /* runs */)
}

func TestScaleFromZero25(t *testing.T) {
	testScaleFromZero(t, 25 /* parallelism */, 5 /* runs */)
}
