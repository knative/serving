/*
Copyright 2022 The Knative Authors

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
	"knative.dev/pkg/environment"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/signals"
	"knative.dev/serving/test/performance/performance"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/serving"

	"golang.org/x/sync/errgroup"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	ktest "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	parallelCount = flag.Int("parallel", 0, "The count of ksvcs we want to run scale-from-zero in parallel")
)

const (
	benchmarkName            = "Serving Serving scale from zero"
	namespace                = "default"
	serviceName              = "perftest-scalefromzero"
	helloWorldExpectedOutput = "Hello World!"
	helloWorldImage          = "helloworld"
	waitToServe              = 2 * time.Minute
)

func main() {
	log.Println("Starting scale from zero test")

	ctx := signals.NewContext()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// To make testing.T work properly
	testing.Init()

	env := environment.ClientConfig{}

	// The local domain is directly resolvable by the test
	flag.Set("resolvabledomain", "true")
	flag.Parse()

	cfg, err := env.GetRESTConfig()
	if err != nil {
		log.Fatalf("failed to get kubeconfig %s", err)
	}

	ctx, _ = injection.EnableInjectionOrDie(ctx, cfg)

	clients, err := test.NewClients(cfg, namespace)
	if err != nil {
		log.Fatal("Failed to setup clients: ", err)
	}

	influxReporter, err := performance.NewInfluxReporter(map[string]string{"parallel": strconv.Itoa(*parallelCount)})
	if err != nil {
		log.Fatalf("Failed to create InfluxReporter: %v", err)
	}

	// Create the services once.
	services, cleanup, err := createServices(clients, *parallelCount)

	// Wrap fatalf in a helper to clean up created resources
	fatalf := func(f string, args ...interface{}) {
		cleanup()
		log.Fatalf(f, args...)
	}
	if err != nil {
		fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	// Wait all services scaling to zero.
	if err := waitForScaleToZero(ctx, services); err != nil {
		fatalf("Failed to wait for all services to scale to zero: %v", err)
	}

	parallelScaleFromZero(ctx, clients, services, influxReporter)

	influxReporter.FlushAndShutdown()

	log.Println("Scale from zero test completed")
}

func createServices(clients *test.Clients, count int) ([]*v1test.ResourceObjects, func(), error) {
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
			test.TearDown(clients, testNames[i])
		}
	}

	objs := make([]*v1test.ResourceObjects, count)
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
			if objs[ndx], err = v1test.CreateServiceReady(&testing.T{}, clients, testNames[ndx], sos...); err != nil {
				return fmt.Errorf("%02d: failed to create Ready service: %w", ndx, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	log.Print("Created all the services in ", time.Since(begin))
	return objs, cleanupNames, nil
}

func waitForScaleToZero(ctx context.Context, objs []*v1test.ResourceObjects) error {
	g := errgroup.Group{}
	for i := 0; i < len(objs); i++ {
		idx := i
		ro := objs[i]
		g.Go(func() error {
			log.Printf("%02d: waiting for deployment to scale to zero", idx)
			selector := labels.SelectorFromSet(labels.Set{
				serving.ServiceLabelKey: ro.Service.Name,
			})

			if err := performance.WaitForScaleToZero(ctx, namespace, selector, 2*time.Minute); err != nil {
				m := fmt.Sprintf("%02d: failed waiting for deployment to scale to zero: %v", idx, err)
				log.Println(m)
				return errors.New(m)
			}
			return nil
		})
	}
	return g.Wait()
}

func parallelScaleFromZero(ctx context.Context, clients *test.Clients, objs []*v1test.ResourceObjects, reporter *performance.InfluxReporter) {
	count := len(objs)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		ndx := i
		go func() {
			defer wg.Done()
			serviceReadyDuration, deploymentUpdatedDuration, err := runScaleFromZero(ctx, clients, ndx, objs[ndx])
			if err == nil {
				reporter.AddDataPoint(benchmarkName, map[string]interface{}{
					"service-ready-latency": serviceReadyDuration.Milliseconds(),
				})
				reporter.AddDataPoint(benchmarkName, map[string]interface{}{
					"deployment-updated-latency": deploymentUpdatedDuration.Milliseconds(),
				})
			} else {
				// Add 1 to the error metric whenever there is an error.
				reporter.AddDataPoint(benchmarkName, map[string]interface{}{
					"errors": float64(1),
				})
			}
		}()
	}
	wg.Wait()
}

func runScaleFromZero(ctx context.Context, clients *test.Clients, idx int, ro *v1test.ResourceObjects) (
	time.Duration, time.Duration, error) {
	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: ro.Service.Name,
	})

	watcher, err := clients.KubeClient.AppsV1().Deployments(namespace).Watch(
		ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		msg := fmt.Sprintf("%02d: unable to watch the deployment for the service: %v", idx, err)
		log.Println(msg)
		return 0, 0, errors.New(msg)
	}
	defer watcher.Stop()

	deploymentChangeChan := watcher.ResultChan()
	serviceReadyChan := make(chan struct{})
	errorChan := make(chan error)

	go func() {
		log.Printf("%02d: waiting for endpoint to serve request", idx)
		url := ro.Route.Status.URL.URL()
		_, err := pkgTest.WaitForEndpointStateWithTimeout(
			ctx,
			clients.KubeClient,
			log.Printf,
			url,
			spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(helloWorldExpectedOutput)),
			"HelloWorldServesText",
			test.ServingFlags.ResolvableDomain, waitToServe,
		)
		if err != nil {
			m := fmt.Sprintf("%02d: the endpoint for Route %q at %q didn't serve the expected text %q: %v", idx, ro.Route.Name, url, helloWorldExpectedOutput, err)
			log.Println(m)
			errorChan <- errors.New(m)
			return
		}

		serviceReadyChan <- struct{}{}
	}()

	start := time.Now()
	// Get the duration that takes to change deployment spec.
	var dd time.Duration
	for {
		select {
		case event := <-deploymentChangeChan:
			if event.Type == watch.Modified {
				dm := event.Object.(*v1.Deployment)
				if *dm.Spec.Replicas != 0 && dd == 0 {
					log.Println("Deployment replicas updated", ro.Service.Name)
					dd = time.Since(start)
				}
			}
		case <-serviceReadyChan:
			log.Println("Service is ready after", ro.Service.Name, time.Since(start))
			return time.Since(start), dd, nil
		case err := <-errorChan:
			log.Println("Service scaling failed: ", ro.Service.Name, err.Error())
			return 0, 0, err
		}
	}
}
