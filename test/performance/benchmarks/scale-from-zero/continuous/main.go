/*
Copyright 2019 The Knative Authors

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

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"knative.dev/pkg/injection/sharedmain"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"
	ktest "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1a1test "knative.dev/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	masterURL = flag.String("master", "", "The address of the Kubernetes API server. "+
		"Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

const (
	testNamespace            = "default"
	serviceName              = "perftest-scalefromzero"
	helloWorldExpectedOutput = "Hello World!"
	helloWorldImage          = "helloworld"
	waitToServe              = 10 * time.Minute
)

func clientsFromFlags() (*test.Clients, error) {
	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("error building kubeconfig: %v", err)
	}
	return test.NewClientsFromConfig(cfg, testNamespace)
}

func createServices(clients *test.Clients, count int) ([]*v1a1test.ResourceObjects, func(), error) {
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
			test.TearDown(clients, *testNames[i])
		}
	}
	test.CleanupOnInterrupt(cleanupNames)

	objs := make([]*v1a1test.ResourceObjects, count)
	begin := time.Now()
	defer func() {
		log.Printf("Total time for test: %v", time.Since(begin))
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
			if objs[ndx], err = v1a1test.CreateRunLatestServiceReady(&testing.T{}, clients, testNames[ndx], sos...); err != nil {
				return fmt.Errorf("%02d: failed to create Ready service: %v", ndx, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	log.Printf("Created all the services in %v", time.Since(begin))
	return objs, cleanupNames, nil
}

func parallelScaleFromZero(t *testing.T, clients *test.Clients, objs []*v1a1test.ResourceObjects, count int) ([]time.Duration, error) {
	g := errgroup.Group{}
	durations := make([]time.Duration, count)
	for i := 0; i < count; i++ {
		ndx := i
		g.Go(func() error {
			dur, err := runScaleFromZero(ndx, clients, objs[ndx])
			t.Logf("%02d: duration: %v, err: %v", ndx, dur, err)
			if err == nil {
				durations[ndx] = dur
			}
			return err
		})
	}
	return durations, g.Wait()
}

func runScaleFromZero(idx int, clients *test.Clients, ro *v1a1test.ResourceObjects) (time.Duration, error) {
	deploymentName := names.Deployment(ro.Revision)

	url := ro.Route.Status.URL.URL()
	log.Printf("%02d: waiting for deployment to scale to zero.", idx)
	if err := e2e.WaitForScaleToZero(t, deploymentName, clients); err != nil {
		m := fmt.Sprintf("%02d: failed waiting for deployment to scale to zero: %v", idx, err)
		log.Println(m)
		return 0, errors.New(m)
	}
	// Wait for scale back to 0
	// if err := performance.WaitForScaleToZero(ctx, namespace, selector, 2*time.Minute); err != nil {
	// 	fatalf("Failed to wait for scale-to-0: %v", err)
	// }

	start := time.Now()
	log.Printf("%02d: waiting for endpoint to serve request", idx)
	if _, err := pkgTest.WaitForEndpointStateWithTimeout(
		clients.KubeClient,
		log.Printf,
		url,
		pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(helloWorldExpectedOutput)),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain, waitToServe,
	); err != nil {
		m := fmt.Sprintf("%02d: the endpoint for Route %q at %q didn't serve the expected text %q: %v", idx, ro.Route.Name, url, helloWorldExpectedOutput, err)
		log.Println(m)
		return 0, errors.New(m)
	}

	log.Printf("%02d: request completed", idx)

	return time.Since(start), nil
}

func testScaleFromZero(count, numRuns int) {
	clients, err := clientsFromFlags()
	if err != nil {
		log.Fatalf("Failed to setup clients: %v", err)
	}
	// Create the services once.
	objs, cleanup, err := createServices(clients, count)
	if err != nil {
		log.Fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	tName := fmt.Sprintf("TestScaleFromZero%02d", count)
	for i := 0; i < numRuns; i++ {
		durs, err := parallelScaleFromZero(t, pc, objs, count)
		if err != nil {
			log.Fatalf("Run %d: %v", i+1, err)
		}
		runStats[i] = getRunStats(durs)
		log.Logf("Run %d: Average: %v", i+1, runStats[i].avg)
	}
}

func main() {
	flag.Parse()

}
