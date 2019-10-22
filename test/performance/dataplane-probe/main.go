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
	"knative.dev/pkg/test/mako"

	"knative.dev/serving/test/performance"
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

	// Accumulate errors per second.  The key is the second at which the
	// error occurred and the value is the count.  In order for this to
	// show up properly (and work with the aggregators) we must report
	// zero values for every second over which the benchmark runs without
	// errors.
	errors := make(map[int64]int64, int(duration.Seconds()))

	// Start the attack!
	results := attacker.Attack(targeter, rate, *duration, "load-test")

LOOP:
	for {
		select {
		case <-ctx.Done():
			// If we timeout or the pod gets shutdown via SIGTERM then start to
			// clean thing up.
			break LOOP

		case res, ok := <-results:
			if !ok {
				// Once we have read all of the request results, break out of
				// our loop.
				break LOOP
			}
			// How much to increment the per-second error count for the time
			// this result's request was sent.
			var isAnError int64
			if res.Error != "" {
				// By reporting errors like this the error strings show up on
				// the details page for each Mako run.
				q.AddError(mako.XTime(res.Timestamp), res.Error)
				isAnError = 1
			} else {
				// Add a sample points for the target benchmark's latency stat
				// with the latency of the request this result is for.
				q.AddSamplePoint(mako.XTime(res.Timestamp), map[string]float64{
					t.stat: res.Latency.Seconds(),
				})
				isAnError = 0
			}
			errors[res.Timestamp.Unix()] += isAnError
		}
	}

	// Walk over our accumulated per-second error rates and report them as
	// sample points.  The key is seconds since the Unix epoch, and the value
	// is the number of errors observed in that second.
	for ts, count := range errors {
		q.AddSamplePoint(mako.XTime(time.Unix(ts, 0)), map[string]float64{
			t.estat: float64(count),
		})
	}

	// Commit data to Mako and handle the result.
	if err := mc.StoreAndHandleResult(); err != nil {
		fatalf("Failed to store and handle benchmarking result: %v", err)
	}
}
