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
	"fmt"
	"log"
	"time"

	"github.com/google/mako/go/quickstore"
	vegeta "github.com/tsenart/vegeta/lib"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/signals"
	pkgpacers "knative.dev/pkg/test/vegeta/pacers"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"

	"knative.dev/pkg/test/mako"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test/performance/metrics"
)

var (
	flavor = flag.String("flavor", "", "The flavor of the benchmark to run.")
)

func processResults(ctx context.Context, q *quickstore.Quickstore, results <-chan *vegeta.Result) {
	// Accumulate the error and request rates along second boundaries.
	errors := make(map[int64]int64)
	requests := make(map[int64]int64)

	// When the benchmark completes, iterate over the accumulated rates
	// and add them as sample points.
	defer func() {
		for t, req := range requests {
			q.AddSamplePoint(mako.XTime(time.Unix(t, 0)), map[string]float64{
				"rs": float64(req),
			})
		}
		for t, err := range errors {
			q.AddSamplePoint(mako.XTime(time.Unix(t, 0)), map[string]float64{
				"es": float64(err),
			})
		}
	}()

	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: fmt.Sprintf("load-test-%s", *flavor),
	})

	ctx, cancel := context.WithCancel(ctx)
	deploymentStatus := metrics.FetchDeploymentStatus(ctx, "default", selector, time.Second)
	sksMode := metrics.FetchSKSMode(ctx, "default", selector, time.Second)
	defer cancel()

	for {
		select {
		case res, ok := <-results:
			// If there are no more results, then we're done!
			if !ok {
				return
			}
			// Handle the result by reporting an error or a latency sample point.
			var isAnError int64
			if res.Error != "" {
				q.AddError(mako.XTime(res.Timestamp), res.Error)
				isAnError = 1
			} else {
				q.AddSamplePoint(mako.XTime(res.Timestamp), map[string]float64{
					"l": res.Latency.Seconds(),
				})
				isAnError = 0
			}
			// Update our error and request rates.
			// We handle errors this way to force zero values into every time for
			// which we have data, even if there is no error.
			errors[res.Timestamp.Unix()] += isAnError
			requests[res.Timestamp.Unix()]++
		case ds := <-deploymentStatus:
			// Add a sample point for the deployment status
			q.AddSamplePoint(mako.XTime(ds.Time), map[string]float64{
				"dp": float64(ds.DesiredReplicas),
				"ap": float64(ds.ReadyReplicas),
			})
		case sksm := <-sksMode:
			// Add a sample point for the serverless service mode
			mode := float64(0)
			if sksm.Mode == netv1alpha1.SKSOperationModeProxy {
				mode = 1.0
			}
			q.AddSamplePoint(mako.XTime(sksm.Time), map[string]float64{
				"sks": mode,
			})
		}
	}
}

func main() {
	flag.Parse()

	if *flavor == "" {
		log.Fatalf("-flavor is a required flag.")
	}

	// We want this for properly handling Kubernetes container lifecycle events.
	ctx := signals.NewContext()

	// We cron every 10 minutes, so give ourselves 6 minutes to complete.
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()

	// Use the benchmark key created.
	tbcTag := "tbc=" + *flavor
	mc, err := mako.Setup(ctx, tbcTag)
	q := mc.Quickstore
	qclose := mc.ShutDownFunc
	ctx = mc.Context
	if err != nil {
		log.Fatalf("failed to setup mako: %v", err)
	}
	// Use a fresh context here so that our RPC to terminate the sidecar
	// isn't subject to our timeout (or we won't shut it down when we time out)
	defer qclose(context.Background())

	q.Input.ThresholdInputs = append(q.Input.ThresholdInputs,
		newLoadTest95PercentileLatency(tbcTag),
		newLoadTestMaximumLatency(tbcTag),
		newLoadTestMaximumErrorRate(tbcTag))

	log.Print("Starting the load test.")
	// Ramp up load from 1k to 3k in 2 minute steps.
	const duration = 2 * time.Minute
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "GET",
		URL:    fmt.Sprintf("http://load-test-%s.default.svc.cluster.local?sleep=100", *flavor),
	})

	pacers := make([]vegeta.Pacer, 3)
	durations := make([]time.Duration, 3)
	for i := 1; i < 4; i++ {
		pacers[i-1] = vegeta.Rate{Freq: i, Per: time.Millisecond}
		durations[i-1] = duration
	}
	pacer, err := pkgpacers.NewCombined(pacers, durations)
	if err != nil {
		qclose(context.Background())
		log.Fatalf("Error creating the pacer: %v", err)
	}
	results := vegeta.NewAttacker().Attack(targeter, pacer, 3*duration, "load-test")
	processResults(ctx, q, results)

	if err := mc.StoreAndHandleResult(); err != nil {
		log.Fatalf("Failed to store and handle benchmarking result: %v", err)
	}
}
