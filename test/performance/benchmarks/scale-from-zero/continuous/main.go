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

	v1 "k8s.io/api/apps/v1"

	"github.com/google/mako/go/quickstore"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	parallelCount = flag.Int("parallel", 0, "The count of ksvcs we want to run scale-from-zero in parallel")
)

const (
	testNamespace            = "default"
	serviceName              = "perftest-scalefromzero"
	helloWorldExpectedOutput = "Hello World!"
	helloWorldImage          = "helloworld"
	waitToServe              = 2 * time.Minute
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

func waitForScaleToZero(ctx context.Context, objs []*v1a1test.ResourceObjects) error {
	g := errgroup.Group{}
	for i := 0; i < len(objs); i++ {
		idx := i
		ro := objs[i]
		g.Go(func() error {
			log.Printf("%02d: waiting for deployment to scale to zero", idx)
			selector := labels.SelectorFromSet(labels.Set{
				serving.ServiceLabelKey: ro.Service.Name,
			})

			if err := performance.WaitForScaleToZero(ctx, testNamespace, selector, 2*time.Minute); err != nil {
				m := fmt.Sprintf("%02d: failed waiting for deployment to scale to zero: %v", idx, err)
				log.Println(m)
				return errors.New(m)
			}
			return nil
		})
	}
	return g.Wait()
}

func parallelScaleFromZero(ctx context.Context, clients *test.Clients, objs []*v1a1test.ResourceObjects, q *quickstore.Quickstore) {
	count := len(objs)
	// Get the key for saving latency and error metrics in the benchmark.
	lk := "l" + strconv.Itoa(count)
	dlk := "dl" + strconv.Itoa(count)
	ek := "e" + strconv.Itoa(count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		ndx := i
		go func() {
			defer wg.Done()
			sdur, ddur, err := runScaleFromZero(ctx, clients, ndx, objs[ndx])
			if err == nil {
				q.AddSamplePoint(mako.XTime(time.Now()), map[string]float64{
					lk: sdur.Seconds(),
				})
				q.AddSamplePoint(mako.XTime(time.Now()), map[string]float64{
					dlk: ddur.Seconds(),
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

func runScaleFromZero(ctx context.Context, clients *test.Clients, idx int, ro *v1a1test.ResourceObjects) (
	time.Duration, time.Duration, error) {
	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: ro.Service.Name,
	})

	watcher, err := clients.KubeClient.Kube.AppsV1().Deployments(testNamespace).Watch(
		metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		m := fmt.Sprintf("%02d: unable to watch the deployment for the service: %v", idx, err)
		log.Println(m)
		return 0, 0, errors.New(m)
	}
	defer watcher.Stop()

	ddch := watcher.ResultChan()
	sdch := make(chan struct{})
	errch := make(chan error)

	go func() {
		log.Printf("%02d: waiting for endpoint to serve request", idx)
		url := ro.Route.Status.URL.URL()
		_, err := pkgTest.WaitForEndpointStateWithTimeout(
			clients.KubeClient,
			log.Printf,
			url,
			pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(helloWorldExpectedOutput)),
			"HelloWorldServesText",
			test.ServingFlags.ResolvableDomain, waitToServe,
		)
		if err != nil {
			m := fmt.Sprintf("%02d: the endpoint for Route %q at %q didn't serve the expected text %q: %v", idx, ro.Route.Name, url, helloWorldExpectedOutput, err)
			log.Println(m)
			errch <- errors.New(m)
			return
		}

		sdch <- struct{}{}
	}()

	start := time.Now()
	// Get the duration that takes to change deployment spec.
	var dd time.Duration
	for {
		select {
		case event := <-ddch:
			if event.Type == watch.Modified {
				dm := event.Object.(*v1.Deployment)
				if *dm.Spec.Replicas != 0 && dd == 0 {
					dd = time.Since(start)
				}
			}
		case <-sdch:
			return time.Since(start), dd, nil
		case err := <-errch:
			return 0, 0, err
		}
	}
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

	// Wait all services scaling to zero.
	if err := waitForScaleToZero(ctx, objs); err != nil {
		fatalf("Failed to wait for all services to scale to zero: %v", err)
	}

	parallelScaleFromZero(ctx, clients, objs, q)
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
