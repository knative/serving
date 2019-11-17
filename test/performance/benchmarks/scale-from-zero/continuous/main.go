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
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/mako/go/quickstore"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/test/mako"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test/performance"

	"golang.org/x/sync/errgroup"

	"knative.dev/pkg/injection/sharedmain"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	ktest "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	parallelCount = flag.Int("parallel", 0, "The count of ksvcs we want to run scale-from-zero in parallel")
)

const (
	testNamespace            = "default"
	serviceName              = "perftest-scalefromzero"
	helloWorldExpectedOutput = "Hello World!"
	helloWorldImage          = "helloworld"
	waitToServe              = 1 * time.Minute
)

func clientsFromConfig() (*test.Clients, error) {
	cfg, err := sharedmain.GetConfig("", "")
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

	objs := make([]*v1a1test.ResourceObjects, count)
	begin := time.Now()
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
			if objs[ndx], _, err = v1a1test.CreateRunLatestServiceReady(&testing.T{}, clients, testNames[ndx], false, sos...); err != nil {
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

func parallelScaleFromZero(ctx context.Context, clients *test.Clients, objs []*v1a1test.ResourceObjects, count int, q *quickstore.Quickstore) {
	// Get the key for saving latency and error metrics in the benchmark.
	lk := "l" + strconv.Itoa(count)
	ek := "e" + strconv.Itoa(count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		ndx := i
		go func() {
			defer wg.Done()
			dur, err := runScaleFromZero(ctx, clients, ndx, objs[ndx])
			log.Printf("%02d: duration: %v, err: %v", ndx, dur, err)
			if err == nil {
				q.AddSamplePoint(mako.XTime(time.Now()), map[string]float64{
					lk: dur.Seconds(),
				})
			} else {
				// Add 1 to the error metric whenever there is an error.
				q.AddSamplePoint(mako.XTime(time.Now()), map[string]float64{
					ek: 1,
				})
				// By reporting errors like this, the error strings show up on
				// the details page for each Mako run.
				q.AddError(mako.XTime(time.Now()), err.Error())
			}
		}()
	}
	wg.Wait()
}

func runScaleFromZero(ctx context.Context, clients *test.Clients, idx int, ro *v1a1test.ResourceObjects) (time.Duration, error) {
	log.Printf("%02d: waiting for deployment to scale to zero.", idx)
	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: ro.Service.Name,
	})

	if err := performance.WaitForScaleToZero(ctx, testNamespace, selector, 2*time.Minute); err != nil {
		m := fmt.Sprintf("%02d: failed waiting for deployment to scale to zero: %v", idx, err)
		log.Println(m)
		return 0, errors.New(m)
	}

	start := time.Now()
	log.Printf("%02d: waiting for endpoint to serve request", idx)
	url := ro.Route.Status.URL.URL()
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

func testScaleFromZero(clients *test.Clients, count int) {
	parallelTag := fmt.Sprintf("parallel=%d", count)
	mc, err := mako.Setup(context.Background(), parallelTag)
	if err != nil {
		log.Fatalf("failed to setup mako: %v", err)
	}
	q, qclose, ctx := mc.Quickstore, mc.ShutDownFunc, mc.Context
	defer qclose(ctx)

	// Create the services once.
	objs, cleanup, err := createServices(clients, count)
	// Wrap fatalf in a helper or our sidecar will live forever, also wrap cleanup.
	fatalf := func(f string, args ...interface{}) {
		cleanup()
		qclose(ctx)
		log.Fatalf(f, args...)
	}
	if err != nil {
		fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	parallelScaleFromZero(ctx, clients, objs, count, q)
	if err := mc.StoreAndHandleResult(); err != nil {
		fatalf("Failed to store and handle benchmarking result: %v", err)
	}
}

func main() {
	flag.Parse()
	clients, err := clientsFromConfig()
	if err != nil {
		log.Fatalf("Failed to setup clients: %v", err)
	}

	testScaleFromZero(clients, *parallelCount)
}
