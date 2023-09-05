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
	"k8s.io/apimachinery/pkg/labels"
	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/environment"
	"knative.dev/pkg/injection"
	"knative.dev/serving/pkg/apis/serving"
	ktest "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/performance/performance"
	v1test "knative.dev/serving/test/v1"

	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/signals"
)

const (
	namespace     = "default"
	benchmarkName = "Knative Serving real traffic test"
	runtimeImage  = "runtime"
	serviceName   = "perftest-"

	duration = 5 * time.Minute

	// vegeta request parameter
	minLatency = 0 * time.Second
	maxLatency = 30 * time.Second

	// init-container
	minStartupLatency = 0 * time.Second
	maxStartupLatency = 60 * time.Second

	// vegeta parameter
	minPayloadSizeKB = 1
	maxPayloadSizeKB = 1000

	// vegeta parameter
	minRequestsPerSecond = 1
	maxRequestPerSecond  = 200
)

var (
	numberOfServices = flag.Int("number-of-services", 100, "The number of Knative Services to create.")
)

type attacker struct {
	attacker          *vegeta.Attacker
	url               string
	latency           int64
	payloadSize       int
	requestsPerSecond int
}

func main() {
	if *numberOfServices <= 0 {
		log.Fatalf("-number-of-services is a required flag.")
	}

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

	influxReporter, err := performance.NewInfluxReporter(map[string]string{"number-of-services": strconv.Itoa(*numberOfServices)})
	if err != nil {
		log.Fatalf("failed to create influx reporter: %v", err.Error())
	}
	defer influxReporter.FlushAndShutdown()

	log.Printf("Creating %d Knative Services", numberOfServices)
	services, cleanup, err := createServices(clients, *numberOfServices)
	if err != nil {
		log.Fatalf("Failed to create services: %v", err)
	}
	defer cleanup()

	// Wrap fatalf in a helper to clean up created resources
	fatalf := func(f string, args ...interface{}) {
		cleanup()
		log.Fatalf(f, args...)
	}

	log.Print("Creating vegeta attackers")
	for _, svc := range services {
		latency := getRandomValue(minLatency.Milliseconds(), maxLatency.Milliseconds())

		cfg := &attacker{
			attacker:          vegeta.NewAttacker(vegeta.Timeout(30 * time.Second)),
			latency:           latency,
			url:               fmt.Sprintf("http://%s.default.svc.cluster.local?sleep=%d", svc.Route, latency),
			payloadSize:       0,
			requestsPerSecond: 0,
		}
	}

	// Ramp up load from 1k to 3k in 2 minute steps.
	const duration = 2 * time.Minute
	url := fmt.Sprintf("http://load-test-%s.default.svc.cluster.local?sleep=100", *flavor)
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: http.MethodGet,
		URL:    url,
	})

	// Make sure the target is ready before sending the large amount of requests.
	if err := performance.ProbeTargetTillReady(url, duration); err != nil {
		log.Fatalf("Failed to get target ready for attacking: %v", err)
	}
	resultsChan := vegeta.NewAttacker().Attack(targeter, pacer, 3*duration, "load-test")
	metricResults := processResults(ctx, resultsChan, influxReporter, selector)

	// Report results to influx
	influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"requests": float64(metricResults.Requests)})
	influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"latency-mean": float64(metricResults.Latencies.Mean)})
	influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"latency-min": float64(metricResults.Latencies.Min)})
	influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"latency-max": float64(metricResults.Latencies.Max)})
	influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"latency-p95": float64(metricResults.Latencies.P95)})
	influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"errors": float64(len(metricResults.Errors))})

	// Report to stdout
	_ = vegeta.NewTextReporter(metricResults).Report(os.Stdout)

	if err := checkSLA(metricResults); err != nil {
		// make sure to still write the stats
		influxReporter.FlushAndShutdown()
		log.Fatalf(err.Error())
	}

	log.Println("Load test finished")
}

func createServices(clients *test.Clients, count int) ([]*v1test.ResourceObjects, func(), error) {
	testNames := make([]*test.ResourceNames, count)

	// Initialize our service names.
	for i := 0; i < count; i++ {
		testNames[i] = &test.ResourceNames{
			Service: test.AppendRandomString(fmt.Sprintf("%s-%02d", serviceName, i)),
			// The crd.go helpers will convert to the actual image path.
			Image: runtimeImage,
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
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
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

func getRandomValue(min, max int64) int64 {
	return rand.Int63n(max-min) + min
}

func processResults(ctx context.Context, results <-chan *vegeta.Result, reporter *performance.InfluxReporter, selector labels.Selector) *vegeta.Metrics {
	ctx, cancel := context.WithCancel(ctx)
	deploymentStatus := performance.FetchDeploymentsStatus(ctx, namespace, selector, time.Second)
	sksMode := performance.FetchSKSStatus(ctx, namespace, selector, time.Second)
	defer cancel()

	metricResults := &vegeta.Metrics{}

	for {
		select {
		case <-ctx.Done():
			// If we time out or the pod gets shutdown via SIGTERM then start to
			// clean thing up.
			return nil

		case res, ok := <-results:
			if ok {
				metricResults.Add(res)
			} else {
				// If there are no more results, then we're done!
				// Compute latency percentiles
				metricResults.Close()

				return metricResults
			}

		case ds := <-deploymentStatus:
			// Add a sample point for the deployment status
			reporter.AddDataPoint(benchmarkName,
				map[string]interface{}{"ready-replicas": float64(ds.ReadyReplicas), "desired-replicas": float64(ds.DesiredReplicas)})

		case sksm := <-sksMode:
			// Add a sample point for the serverless service mode
			mode := float64(0)
			if sksm.Mode == netv1alpha1.SKSOperationModeProxy {
				mode = 1.0
			}
			reporter.AddDataPoint(benchmarkName,
				map[string]interface{}{"sks": mode, "num-activators": float64(sksm.NumActivators)})
		}
	}
}

func checkSLA(results *vegeta.Metrics) error {
	// SLA 1: the p95 latency has to be over the 0->3k stepped burst
	// falls in the +15ms range (we sleep 100 ms, so 100-115ms).
	// This includes a mix of cold-starts and steady state (once the autoscaling decisions have leveled off).
	if results.Latencies.P95 >= 100*time.Millisecond && results.Latencies.P95 <= 115*time.Millisecond {
		log.Println("SLA 1 passed. P95 latency is in 100-115ms time range")
	} else {
		return fmt.Errorf("SLA 1 failed. P95 latency is not in 100-115ms time range: %s", results.Latencies.P95)
	}

	// SLA 2: the maximum request latency observed over the 0->3k
	// stepped burst is no more than +10 seconds. This is not strictly a cold-start
	// metric, but it is a superset that includes steady state latency and the latency
	// of non-cold-start overload requests.
	if results.Latencies.Max <= 10*time.Second {
		log.Println("SLA 2 passed. Max latency is below 10s")
	} else {
		return fmt.Errorf("SLA 2 failed. Max latency is above 10s: %s", results.Latencies.Max)
	}

	// SLA 3: The mean error rate observed over the 0->3k stepped burst is 0.
	if len(results.Errors) == 0 {
		log.Println("SLA 3 passed. No errors occurred")
	} else {
		return fmt.Errorf("SLA 3 failed. Errors occurred: %d", len(results.Errors))
	}

	return nil
}
