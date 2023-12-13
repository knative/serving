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
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	v1 "k8s.io/api/apps/v1"
	netapi "knative.dev/networking/pkg/apis/networking"
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

const (
	benchmarkName            = "Knative Serving scale from zero"
	namespace                = "default"
	serviceName              = "perftest-scalefromzero"
	helloWorldExpectedOutput = "Hello World!"
	helloWorldImage          = "helloworld"
	waitToServe              = 5 * time.Minute
)

var (
	parallelCount = flag.Int("parallel", 0, "The count of ksvcs we want to run scale-from-zero in parallel")

	// Map the above to our benchmark targets.
	slas = map[int]struct {
		p95min     time.Duration
		p95max     time.Duration
		latencyMax time.Duration
	}{
		1: {
			p95min:     0,
			p95max:     50 * time.Millisecond,
			latencyMax: 50 * time.Millisecond,
		},
		5: {
			p95min:     0,
			p95max:     50 * time.Millisecond,
			latencyMax: 50 * time.Millisecond,
		},
		25: {
			// Scaling 25 services in parallel will hit some API limits, which cause a step
			// in the deployment updated time which adds to the total time until a service is ready
			p95min:     0,
			p95max:     2 * time.Second,
			latencyMax: 5 * time.Second,
		},
		100: {
			// Scaling 100 services in parallel will hit some API limits, which cause a step
			// in the deployment updated time which adds to the total time until a service is ready
			p95min:     0,
			p95max:     25 * time.Second,
			latencyMax: 25 * time.Second,
		},
	}
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
	defer influxReporter.FlushAndShutdown()

	// We use vegeta.Metrics here as a metrics collector because it already contains logic to calculate percentiles
	vegetaReporter := performance.NewVegetaReporter()

	// Create the services once.
	services, cleanup, err := createServices(clients, *parallelCount)
	if err != nil {
		log.Fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	// Wrap fatalf in a helper to clean up created resources
	fatalf := func(f string, args ...interface{}) {
		cleanup()
		vegetaReporter.StopAndCollectMetrics()
		log.Fatalf(f, args...)
	}

	// Wait all services scaling to zero.
	if err := waitForScaleToZero(ctx, services); err != nil {
		fatalf("Failed to wait for all services to scale to zero: %v", err)
	}

	parallelScaleFromZero(ctx, clients, services, influxReporter, vegetaReporter)

	metricResults := vegetaReporter.StopAndCollectMetrics()

	// Report the results
	influxReporter.AddDataPointsForMetrics(metricResults, benchmarkName)
	_ = vegeta.NewTextReporter(metricResults).Report(os.Stdout)

	sla := slas[*parallelCount]
	if err := checkSLA(metricResults, sla.p95min, sla.p95max, sla.latencyMax, *parallelCount); err != nil {
		// make sure to still write the stats
		influxReporter.FlushAndShutdown()
		log.Fatalf(err.Error())
	}

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
		ktest.WithServiceLabel(netapi.VisibilityLabelKey, serving.VisibilityClusterLocal),
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

func parallelScaleFromZero(ctx context.Context, clients *test.Clients, objs []*v1test.ResourceObjects, reporter *performance.InfluxReporter, vegetaReporter *performance.VegetaReporter) {
	count := len(objs)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		ndx := i
		go func() {
			defer wg.Done()
			serviceReadyDuration, deploymentUpdatedDuration, err := runScaleFromZero(ctx, clients, ndx, objs[ndx])
			if err == nil {
				vegetaReporter.AddResult(&vegeta.Result{Latency: serviceReadyDuration})
				reporter.AddDataPoint(benchmarkName, map[string]interface{}{
					"service-ready-latency": serviceReadyDuration.Milliseconds(),
				})
				reporter.AddDataPoint(benchmarkName, map[string]interface{}{
					"deployment-updated-latency": deploymentUpdatedDuration.Milliseconds(),
				})
			} else {
				// Add 1 to the error metric whenever there is an error.
				reporter.AddDataPoint(benchmarkName, map[string]interface{}{
					"errors": 1,
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
					dd = time.Since(start)
				}
			}
		case <-serviceReadyChan:
			since := time.Since(start)
			log.Printf("Service is ready after: name: %s, deployment-updated: %vms, service-ready-since-deployment: %vms, service-ready-total: %vms",
				ro.Service.Name, dd.Milliseconds(), (since - dd).Milliseconds(), since.Milliseconds())
			return since, dd, nil
		case err := <-errorChan:
			log.Println("Service scaling failed: ", ro.Service.Name, err.Error())
			return 0, 0, err
		}
	}
}

func checkSLA(results *vegeta.Metrics, p95min time.Duration, p95max time.Duration, latencyMax time.Duration, parallel int) error {
	// SLA 1: The p95 latency hitting the target has to be between the range defined
	if results.Latencies.P95 >= p95min && results.Latencies.P95 <= p95max {
		log.Printf("SLA 1 passed. P95 latency is in %d-%dms time range", p95min, p95max)
	} else {
		return fmt.Errorf("SLA 1 failed. P95 latency is not in %d-%dms time range: %s", p95min, p95max, results.Latencies.P95)
	}

	// SLA 2: The max latency hitting the target has to be between the range defined
	if results.Latencies.Max <= latencyMax {
		log.Printf("SLA 2 passed. Max latency is below or equal to %dms", latencyMax)
	} else {
		return fmt.Errorf("SLA 2 failed. Max latency is higher than %dms: %s", latencyMax, results.Latencies.Max)
	}

	// SLA 3: making sure the defined vegeta total requests is met, the defined vegeta total requests should equal to the count of ksvcs we want to run scale-from-zero in parallel
	if results.Requests == uint64(parallel) {
		log.Printf("SLA 3 passed. total requests is %d", results.Requests)
	} else {
		return fmt.Errorf("SLA 3 failed. total requests is %d, expected total requests is %d", results.Requests, uint64(parallel))
	}

	return nil
}
