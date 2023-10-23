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
	"knative.dev/pkg/injection"
	"knative.dev/serving/test/performance/performance"

	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
)

const (
	benchmarkName = "Knative Serving dataplane probe"
)

var (
	target     = flag.String("target", "", "The target to attack.")
	duration   = flag.Duration("duration", 5*time.Minute, "The duration of the probe")
	minDefault = 100 * time.Millisecond
)

var (
	// Map the above to our benchmark targets.
	targets = map[string]struct {
		target vegeta.Target
		slaMin time.Duration
		slaMax time.Duration
	}{
		"deployment": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://deployment.default.svc.cluster.local?sleep=100",
			},
			// vanilla deployment falls in the +5ms range. This does not have Knative or Istio components
			// on the dataplane, and so it is intended as a canary to flag environmental
			// problems that might be causing contemporaneous Knative or Istio runs to fall out of SLA.
			slaMin: minDefault,
			slaMax: 105 * time.Millisecond,
		},
		"queue": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy.default.svc.cluster.local?sleep=100",
			},
			// hitting a Knative Service
			// going through JUST the queue-proxy falls in the +10ms range.
			slaMin: minDefault,
			slaMax: 110 * time.Millisecond,
		},
		"activator": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator.default.svc.cluster.local?sleep=100",
			},
			// hitting a Knative Service
			// going through BOTH the activator and queue-proxy falls in the +10ms range.
			slaMin: minDefault,
			slaMax: 110 * time.Millisecond,
		},
	}
)

func main() {
	ctx := signals.NewContext()
	cfg := injection.ParseAndGetRESTConfigOrDie()
	ctx, startInformers := injection.EnableInjectionOrDie(ctx, cfg)
	startInformers()

	if *target == "" {
		log.Fatalf("-target is a required flag.")
	}

	log.Println("Starting dataplane probe for target:", *target)

	ctx, cancel := context.WithTimeout(ctx, *duration+time.Minute)
	defer cancel()

	// Based on the "target" flag, load up our target benchmark.
	// We only run one variation per run to avoid the runs being noisy neighbors,
	// which in early iterations of the benchmark resulted in latency bleeding
	// across the different workload types.
	t, ok := targets[*target]
	if !ok {
		log.Fatalf("Unrecognized target: %s", *target)
	}

	// Make sure the target is ready before sending the large amount of requests.
	if err := performance.ProbeTargetTillReady(t.target.URL, *duration); err != nil {
		log.Fatalf("Failed to get target ready for attacking: %v", err)
	}

	// Send 1000 QPS (1 per ms) for the given duration with a 30s request timeout.
	rate := vegeta.Rate{Freq: 1, Per: time.Millisecond}
	targeter := vegeta.NewStaticTargeter(t.target)
	attacker := vegeta.NewAttacker(vegeta.Timeout(30 * time.Second))

	influxReporter, err := performance.NewInfluxReporter(map[string]string{"target": *target})
	if err != nil {
		log.Fatalf("failed to create influx reporter: %v", err.Error())
	}
	defer influxReporter.FlushAndShutdown()

	// Start the attack!
	results := attacker.Attack(targeter, rate, *duration, "load-test")
	deploymentStatus := performance.FetchDeploymentStatus(ctx, system.Namespace(), "activator", time.Second)

	metricResults := &vegeta.Metrics{}

LOOP:
	for {
		select {
		case <-ctx.Done():
			// If we time out or the pod gets shutdown via SIGTERM then start to
			// clean thing up.
			break LOOP

		case ds := <-deploymentStatus:
			// Report number of ready activators.
			influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{"activator-pod-count": ds.ReadyReplicas})

		case res, ok := <-results:
			if ok {
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

	if err := checkSLA(metricResults, t.slaMin, t.slaMax, rate, *duration); err != nil {
		// make sure to still write the stats
		influxReporter.FlushAndShutdown()
		log.Fatalf(err.Error())
	}

	log.Println("Dataplane probe test finished")
}

func checkSLA(results *vegeta.Metrics, slaMin time.Duration, slaMax time.Duration, rate vegeta.ConstantPacer, duration time.Duration) error {
	// SLA 1: The p95 latency hitting the target has to be between the range defined
	// in the target map on top.
	if results.Latencies.P95 >= slaMin && results.Latencies.P95 <= slaMax {
		log.Printf("SLA 1 passed. P95 latency is in %d-%dms time range", slaMin, slaMax)
	} else {
		return fmt.Errorf("SLA 1 failed. P95 latency is not in %d-%dms time range: %s", slaMin, slaMax, results.Latencies.P95)
	}

	// SLA 2: making sure the defined total request is met
	if results.Requests == uint64(rate.Rate(time.Second)*duration.Seconds()) {
		log.Printf("SLA 2 passed. vegeta total request is %d", results.Requests)
	} else {
		return fmt.Errorf("SLA 2 failed. vegeta total request is %d, expected total request is %f", results.Requests, rate.Rate(time.Second)*duration.Seconds())
	}

	return nil
}
