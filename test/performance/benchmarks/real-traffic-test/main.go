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
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/environment"
	"knative.dev/pkg/injection"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	ktest "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/performance/performance"
	v1test "knative.dev/serving/test/v1"

	"knative.dev/pkg/signals"

	crytporand "crypto/rand"
)

const (
	namespace     = "default"
	benchmarkName = "Knative Serving real traffic test"
	serviceName   = "perftest"

	duration = 5 * time.Minute

	// Test configuration
	// Defines the latency of target
	minLatency = 0 * time.Second
	maxLatency = 5 * time.Second

	// Defines the delay that a Knative Service has for startup (init-container causes the delay)
	minStartupLatency = 0 * time.Second
	maxStartupLatency = 10 * time.Second

	// Defines the payloads that are sent on the vegeta requests
	minPayloadSizeBytes = 10
	maxPayloadSizeBytes = 50_000
)

var (
	numberOfServices = flag.Int("number-of-services", 10, "The number of Knative Services to create")
	rps              = flag.Int("requests-per-second", 300, "The of requests per second to send")
)

type serviceConfig struct {
	resourceObjects *v1test.ResourceObjects

	activatorAlwaysInPath bool
	latency               int64
	startupLatency        int64
	payload               []byte
}

func main() {
	if *rps >= 2500 {
		log.Fatal("One test container cannot create more than 2500 RPS without errors. Consider starting this test in parallel.")
	}

	ctx := signals.NewContext()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// To make testing.T work properly
	testing.Init()

	env := environment.ClientConfig{}

	// The local domain is directly resolvable by the test
	flag.Set("resolvabledomain", "true")

	// manually parse flags to avoid conflicting flags
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

	influxReporter, err := performance.NewInfluxReporter(map[string]string{"number-of-services": strconv.Itoa(*numberOfServices)})
	if err != nil {
		log.Fatalf("failed to create influx reporter: %v", err.Error())
	}
	defer influxReporter.FlushAndShutdown()

	log.Printf("Creating %d Knative Services", *numberOfServices)
	services, cleanup, err := createServices(clients, *numberOfServices)
	if err != nil {
		log.Fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	log.Print("Creating vegeta targets")

	targets := []vegeta.Target{}
	for _, svc := range services {
		t := vegeta.Target{
			Method: http.MethodPost,
			URL:    fmt.Sprintf("http://%s.default.svc.cluster.local?sleep=%d", svc.resourceObjects.Service.Name, svc.latency),
			Body:   svc.payload,
		}
		targets = append(targets, t)

		log.Printf("Name: %s, startup-latency: %ds, latency: %dms, payload: %dKb, activator-always-in-path: %v",
			svc.resourceObjects.Service.Name, svc.startupLatency, svc.latency, len(svc.payload)/1000, svc.activatorAlwaysInPath)
	}

	// Send configured RPS round-robin over all services,
	// while the request timeout is based on the max delays + 20 seconds
	log.Printf("Starting vegeta attack for with %v RPS for duration: %v", *rps, duration)
	rate := vegeta.Rate{Freq: *rps, Per: time.Second}
	attacker := vegeta.NewAttacker(vegeta.Timeout(maxLatency + maxStartupLatency + 20*time.Second))
	targeter := vegeta.NewStaticTargeter(targets...)

	results := attacker.Attack(targeter, rate, duration, "real-traffic-test")

	metricResults := &vegeta.Metrics{}

LOOP:
	for {
		select {
		case <-ctx.Done():
			// If we time out or the pod gets shutdown via SIGTERM then start to
			// clean thing up.
			break LOOP

		case res, ok := <-results:
			if ok {
				if res.Error != "" {
					log.Printf("error occurred calling target. Err: %s, url: %s, method: %s", res.Error, res.URL, res.Method)
				}
				metricResults.Add(res)
			} else {
				// If there are no more results, then we're done!
				break LOOP
			}
		}
	}

	// Compute latency percentiles
	metricResults.Close()

	// Report the results
	influxReporter.AddDataPointsForMetrics(metricResults, benchmarkName)
	_ = vegeta.NewTextReporter(metricResults).Report(os.Stdout)

	if err := checkSLA(metricResults, rate); err != nil {
		cleanup()
		influxReporter.FlushAndShutdown()
		log.Fatal(err.Error())
	}

	log.Println("Real traffic test finished")
}

func createServices(clients *test.Clients, count int) ([]*serviceConfig, func(), error) {
	testNames := make([]*test.ResourceNames, count)

	// Initialize our service names.
	for i := 0; i < count; i++ {
		testNames[i] = &test.ResourceNames{
			Service: test.AppendRandomString(fmt.Sprintf("%s-%02d", serviceName, i)),
			// The crd.go helpers will convert to the actual image path.
			Image: test.Runtime,
		}
	}

	cleanupNames := func() {
		log.Println("Cleaning up all created services")
		for i := 0; i < count; i++ {
			test.TearDown(clients, testNames[i])
		}
	}

	objs := make([]*serviceConfig, count)
	begin := time.Now()
	sos := []ktest.ServiceOption{
		ktest.WithResourceRequirements(corev1.ResourceRequirements{
			// We set a small resource alloc so that we can pack more pods into the cluster,
			// also we do not set limits, as buffering in QP will take memory, and we'd be OOMKilled.
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("20Mi"),
			},
		}),
		ktest.WithServiceLabel(netapi.VisibilityLabelKey, serving.VisibilityClusterLocal),
	}

	g := errgroup.Group{}
	for i := 0; i < count; i++ {
		ndx := i
		g.Go(func() error {
			activatorInPath := getRandomBool()
			if activatorInPath {
				annotations := map[string]string{autoscaling.TargetBurstCapacityKey: "-1"}
				sos = append(sos, ktest.WithConfigAnnotations(annotations))
			}

			startupLatency := getRandomValue(int64(minStartupLatency.Seconds()), int64(maxStartupLatency.Seconds()))
			if startupLatency > 0 {
				sos = append(sos, ktest.WithInitContainer(corev1.Container{
					Name:  "slow-startup",
					Image: pkgTest.ImagePath(test.SlowStart),
					Args:  []string{"-sleep", strconv.FormatInt(startupLatency, 10)},
				}))
			}

			createdService, err := v1test.CreateServiceReady(&testing.T{}, clients, testNames[ndx], sos...)
			if err != nil {
				return fmt.Errorf("%02d: failed to create Ready service: %w", ndx, err)
			}
			latency := getRandomValue(minLatency.Milliseconds(), maxLatency.Milliseconds())
			payload, err := getRandomPayload(minPayloadSizeBytes, maxPayloadSizeBytes)
			if err != nil {
				return fmt.Errorf("%02d: failed to generate random payload: %w", ndx, err)
			}
			objs[ndx] = &serviceConfig{
				resourceObjects:       createdService,
				activatorAlwaysInPath: activatorInPath,
				latency:               latency,
				payload:               payload,
				startupLatency:        startupLatency,
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

func getRandomPayload(min int, max int) ([]byte, error) {
	num := getRandomValue(int64(min), int64(max))

	buf := make([]byte, num)
	_, err := crytporand.Read(buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func getRandomValue(min, max int64) int64 {
	return rand.Int63n(max-min) + min
}

func getRandomBool() bool {
	return rand.Intn(2) == 1
}

func checkSLA(results *vegeta.Metrics, rate vegeta.ConstantPacer) error {
	// SLA 1: All requests should pass successfully.
	if len(results.Errors) == 0 {
		log.Println("SLA 1 passed. No errors occurred")
	} else {
		return fmt.Errorf("SLA 1 failed. Errors occurred: %d", len(results.Errors))
	}

	// SLA 2: making sure the defined vegeta rates is met
	if results.Rate == rate.Rate(time.Second) {
		log.Printf("SLA 2 passed. vegeta rate is %f", rate.Rate(time.Second))
	} else {
		return fmt.Errorf("SLA 2 failed. vegeta rate is %f, expected Rate is %f", results.Rate, rate.Rate(time.Second))
	}

	return nil
}
