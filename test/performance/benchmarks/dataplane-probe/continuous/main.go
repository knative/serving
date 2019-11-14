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
	"flag"
	"log"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"

	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/test/mako"
	"knative.dev/serving/test/performance"
	"knative.dev/serving/test/performance/metrics"
)

var (
	target   = flag.String("target", "", "The target to attack.")
	duration = flag.Duration("duration", 5*time.Minute, "The duration of the probe")
)

func main() {
	flag.Parse()

	// We want this for properly handling Kubernetes container lifecycle events.
	ctx := signals.NewContext()

	// We cron quite often, so make sure that we don't severely overrun to
	// limit how noisy a neighbor we can be.
	ctx, cancel := context.WithTimeout(ctx, *duration+time.Minute)
	defer cancel()

	// Use the benchmark key created
	mc, err := mako.Setup(ctx)
	if err != nil {
		log.Fatalf("Failed to setup mako: %v", err)
	}
	q, qclose, ctx := mc.Quickstore, mc.ShutDownFunc, mc.Context
	// Use a fresh context here so that our RPC to terminate the sidecar
	// isn't subject to our timeout (or we won't shut it down when we time out)
	defer qclose(context.Background())

	// Wrap fatalf in a helper or our sidecar will live forever.
	fatalf := func(f string, args ...interface{}) {
		qclose(context.Background())
		log.Fatalf(f, args...)
	}

	// Validate flags after setting up "fatalf" or our sidecar will run forever.
	if *target == "" {
		fatalf("Missing flag: -target")
	}

	// Based on the "target" flag, load up our target benchmark.
	// We only run one variation per run to avoid the runs being noisy neighbors,
	// which in early iterations of the benchmark resulted in latency bleeding
	// across the different workload types.
	t, ok := targets[*target]
	if !ok {
		fatalf("Unrecognized target: %s", *target)
	}

	// Make sure the target is ready before sending the large amount of requests.
	if err := performance.ProbeTargetTillReady(t.target.URL, *duration); err != nil {
		fatalf("Failed to get target ready for attacking: %v", err)
	}

	// Set up the threshold analyzers for the selected benchmark.  This will
	// cause Mako/Quickstore to analyze the results we are storing and flag
	// things that are outside of expected bounds.
	q.Input.ThresholdInputs = append(q.Input.ThresholdInputs, t.analyzers...)

	// Send 1000 QPS (1 per ms) for the given duration with a 30s request timeout.
	rate := vegeta.Rate{Freq: 1, Per: time.Millisecond}
	targeter := vegeta.NewStaticTargeter(t.target)
	attacker := vegeta.NewAttacker(vegeta.Timeout(30 * time.Second))

	// Create a new aggregateResult to accumulate the results.
	ar := metrics.NewAggregateResult(int(duration.Seconds()))

	// Start the attack!
	results := attacker.Attack(targeter, rate, *duration, "load-test")
	deploymentStatus := metrics.FetchDeploymentStatus(ctx, system.Namespace(), "activator", time.Second)
LOOP:
	for {
		select {
		case <-ctx.Done():
			// If we timeout or the pod gets shutdown via SIGTERM then start to
			// clean thing up.
			break LOOP

		case ds := <-deploymentStatus:
			// Report number of ready activators.
			q.AddSamplePoint(mako.XTime(ds.Time), map[string]float64{
				"ap": float64(ds.ReadyReplicas),
			})
		case res, ok := <-results:
			if !ok {
				// Once we have read all of the request results, break out of
				// our loop.
				break LOOP
			}
			// Handle the result for this request
			metrics.HandleResult(q, *res, t.stat, ar)
		}
	}

	// Walk over our accumulated per-second error rates and report them as
	// sample points.  The key is seconds since the Unix epoch, and the value
	// is the number of errors observed in that second.
	for ts, count := range ar.ErrorRates {
		q.AddSamplePoint(mako.XTime(time.Unix(ts, 0)), map[string]float64{
			t.estat: float64(count),
		})
	}

	// Commit data to Mako and handle the result.
	if err := mc.StoreAndHandleResult(); err != nil {
		fatalf("Failed to store and handle benchmarking result: %v", err)
	}
}
