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
	"strings"
	"time"

	"github.com/google/mako/helpers/go/quickstore"
	qpb "github.com/google/mako/helpers/proto/quickstore/quickstore_go_proto"
	vegeta "github.com/tsenart/vegeta/lib"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/clientcmd"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/clients/kubeclient"
	"knative.dev/pkg/signals"
	pkgpacers "knative.dev/pkg/test/vegeta/pacers"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"

	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test/performance/mako"
	"knative.dev/serving/test/performance/metrics"
)

var (
	flavor     = flag.String("flavor", "", "The flavor of the benchmark to run.")
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
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

	stopDeploymentCh := make(chan struct{})
	deploymentStatus := metrics.FetchDeploymentStatus(ctx, "default", selector, time.Second, stopDeploymentCh)
	stopSKSCh := make(chan struct{})
	sksMode := metrics.FetchSKSMode(ctx, "default", selector, time.Second, stopSKSCh)
	// When the benchmark completes, stop fetching deployment and serverless service status.
	defer func() {
		stopDeploymentCh <- struct{}{}
		stopSKSCh <- struct{}{}
	}()

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

	// We want this for properly handling Kubernetes container lifecycle events.
	ctx := signals.NewContext()

	// Setup a deployment informer, so that we can use the lister to track
	// desired and available pod counts.
	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}
	ctx, informers := injection.Default.SetupInformers(ctx, cfg)
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		log.Fatalf("Failed to start informers: %v", err)
	}

	// Get the Kubernetes version from the API server.
	version, err := kubeclient.Get(ctx).Discovery().ServerVersion()
	if err != nil {
		log.Fatalf("Failed to fetch kubernetes version: %v", err)
	}

	// We cron every 10 minutes, so give ourselves 6 minutes to complete.
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()

	if *flavor == "" {
		log.Fatalf("-flavor is a required flag.")
	}

	// Use the benchmark key created
	q, qclose, err := quickstore.NewAtAddress(ctx, &qpb.QuickstoreInput{
		BenchmarkKey: mako.MustGetBenchmark(),
		Tags: []string{
			"master",
			fmt.Sprintf("tbc=%s", *flavor),
			// The format of version.String() is like "v1.13.7-gke.8",
			// since Mako does not allow . in tags, replace them with _
			fmt.Sprintf("kubernetes=%s", strings.ReplaceAll(version.String(), ".", "_")),
		},
	}, mako.SidecarAddress)
	if err != nil {
		log.Fatalf("failed NewAtAddress: %v", err)
	}
	// Use a fresh context here so that our RPC to terminate the sidecar
	// isn't subject to our timeout (or we won't shut it down when we time out)
	defer qclose(context.Background())

	q.Input.ThresholdInputs = append(q.Input.ThresholdInputs,
		LoadTest95PercentileLatency, LoadTestMaximumLatency)

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

	out, err := q.Store()
	if err != nil {
		qclose(context.Background())
		log.Fatalf("q.Store error: %s %v", out.String(), err)
	}
	log.Printf("Done! Run: %s", out.GetRunChartLink())
}
