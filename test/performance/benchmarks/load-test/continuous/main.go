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
	"net/http"
	"time"

	"github.com/google/mako/go/quickstore"
	vegeta "github.com/tsenart/vegeta/lib"
	"k8s.io/apimachinery/pkg/labels"

	"knative.dev/pkg/signals"
	"knative.dev/pkg/test/mako"
	pkgpacers "knative.dev/pkg/test/vegeta/pacers"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test/performance"
	"knative.dev/serving/test/performance/metrics"
)

const namespace = "default"

var (
	flavor   = flag.String("flavor", "", "The flavor of the benchmark to run.")
	selector labels.Selector
)

func processResults(ctx context.Context, q *quickstore.Quickstore, results <-chan *vegeta.Result) {
	// Create a new aggregateResult to accumulate the results.
	ar := metrics.NewAggregateResult(0)

	// When the benchmark completes, iterate over the accumulated rates
	// and add them as sample points.
	defer func() {
		for t, req := range ar.RequestRates {
			q.AddSamplePoint(mako.XTime(time.Unix(t, 0)), map[string]float64{
				"rs": float64(req),
			})
		}
		for t, err := range ar.ErrorRates {
			q.AddSamplePoint(mako.XTime(time.Unix(t, 0)), map[string]float64{
				"es": float64(err),
			})
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	deploymentStatus := metrics.FetchDeploymentsStatus(ctx, namespace, selector, time.Second)
	sksMode := metrics.FetchSKSMode(ctx, namespace, selector, time.Second)
	defer cancel()

	for {
		select {
		case res, ok := <-results:
			// If there are no more results, then we're done!
			if !ok {
				return
			}
			// Handle the result for this request
			metrics.HandleResult(q, *res, "l", ar)
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
	selector = labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: "load-test-" + *flavor,
	})

	// We want this for properly handling Kubernetes container lifecycle events.
	ctx := signals.NewContext()

	// We cron every 10 minutes, so give ourselves 8 minutes to complete.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	// Use the benchmark key created.
	tbcTag := "tbc=" + *flavor
	mc, err := mako.Setup(ctx, tbcTag)
	if err != nil {
		log.Fatalf("failed to setup mako: %v", err)
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

	q.Input.ThresholdInputs = append(q.Input.ThresholdInputs,
		newLoadTest95PercentileLatency(tbcTag),
		newLoadTestMaximumLatency(tbcTag),
		newLoadTestMaximumErrorRate(tbcTag))

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
		fatalf("Failed to get target ready for attacking: %v", err)
	}
	// Wait for scale back to 0
	if err := performance.WaitForScaleToZero(ctx, namespace, selector, 2*time.Minute); err != nil {
		fatalf("Failed to wait for scale-to-0: %v", err)
	}

	pacers := make([]vegeta.Pacer, 3)
	durations := make([]time.Duration, 3)
	for i := 1; i < 4; i++ {
		pacers[i-1] = vegeta.Rate{Freq: i, Per: time.Millisecond}
		durations[i-1] = duration
	}
	pacer, err := pkgpacers.NewCombined(pacers, durations)
	if err != nil {
		fatalf("Error creating the pacer: %v", err)
	}
	results := vegeta.NewAttacker().Attack(targeter, pacer, 3*duration, "load-test")
	processResults(ctx, q, results)

	if err := mc.StoreAndHandleResult(); err != nil {
		fatalf("Failed to store and handle benchmarking result: %v", err)
	}
}
