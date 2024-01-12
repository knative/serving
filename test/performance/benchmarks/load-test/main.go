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
	"net/http"
	"os"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/injection"
	"knative.dev/serving/test/performance/performance"

	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/signals"
	pkgpacers "knative.dev/pkg/test/vegeta/pacers"
	"knative.dev/serving/pkg/apis/serving"
)

const (
	namespace     = "default"
	benchmarkName = "Knative Serving load test"
)

var (
	flavor = flag.String("flavor", "", "The flavor of the benchmark to run.")
)

func main() {
	ctx := signals.NewContext()
	cfg := injection.ParseAndGetRESTConfigOrDie()
	ctx, startInformers := injection.EnableInjectionOrDie(ctx, cfg)
	startInformers()

	if *flavor == "" {
		log.Fatalf("-flavor is a required flag.")
	}
	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: "load-test-" + *flavor,
	})

	// We cron every 10 minutes, so give ourselves 8 minutes to complete.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	influxReporter, err := performance.NewInfluxReporter(map[string]string{"flavor": *flavor})
	if err != nil {
		log.Fatalf("failed to create influx reporter: %v", err.Error())
	}
	defer influxReporter.FlushAndShutdown()

	log.Print("Starting the load test.")
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
	// Wait for scale back to 0
	if err := performance.WaitForScaleToZero(ctx, namespace, selector, 2*time.Minute); err != nil {
		log.Fatalf("Failed to wait for scale-to-0: %v", err)
	}

	// Run vegeta attack with custom pacers and process results from channel
	pacers := make([]vegeta.Pacer, 3)
	durations := make([]time.Duration, 3)
	for i := 1; i < 4; i++ {
		pacers[i-1] = vegeta.Rate{Freq: i, Per: time.Millisecond}
		durations[i-1] = duration
	}
	pacer, err := pkgpacers.NewCombined(pacers, durations)
	if err != nil {
		log.Fatalf("Error creating the pacer: %v", err)
	}
	resultsChan := vegeta.NewAttacker().Attack(targeter, pacer, 3*duration, "load-test")
	metricResults := processResults(ctx, resultsChan, influxReporter, selector)

	// Report the results
	influxReporter.AddDataPointsForMetrics(metricResults, benchmarkName)
	_ = vegeta.NewTextReporter(metricResults).Report(os.Stdout)

	if err := checkSLA(metricResults, pacers, durations); err != nil {
		// make sure to still write the stats
		influxReporter.FlushAndShutdown()
		log.Fatalf(err.Error())
	}

	log.Println("Load test finished")
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
				map[string]interface{}{"ready-replicas": ds.ReadyReplicas, "desired-replicas": ds.DesiredReplicas})

		case sksm := <-sksMode:
			// Add a sample point for the serverless service mode
			mode := float64(0)
			if sksm.Mode == netv1alpha1.SKSOperationModeProxy {
				mode = 1.0
			}
			reporter.AddDataPoint(benchmarkName,
				map[string]interface{}{"sks": mode, "num-activators": sksm.NumActivators})
		}
	}
}

func checkSLA(results *vegeta.Metrics, pacers []vegeta.Pacer, durations []time.Duration) error {
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

	// SLA 4: making sure the defined vegeta total requests is met
	var expectedSum float64
	var expectedRequests uint64
	for i := 0; i < len(pacers); i++ {
		expectedSum = expectedSum + pacers[i].Rate(time.Second)*durations[i].Seconds()
	}
	expectedRequests = uint64(expectedSum)
	if results.Requests >= expectedRequests-(expectedRequests/1000) {
		log.Printf("SLA 4 passed. total requests is %d, expected threshold is %d", results.Requests, expectedRequests-(expectedRequests/1000))
	} else {
		return fmt.Errorf("SLA 4 failed. total requests is %d, expected threshold is %d", results.Requests, expectedRequests-(expectedRequests/1000))
	}

	return nil
}
