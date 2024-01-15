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
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"knative.dev/pkg/injection"
	"knative.dev/serving/test/performance/performance"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	servingclient "knative.dev/serving/pkg/client/injection/client"

	"knative.dev/pkg/signals"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
)

var (
	target   = flag.String("target", "", "The target to attack.")
	duration = flag.Duration("duration", 5*time.Minute, "The duration of the probe")
)

var (
	// Map the above to our benchmark targets.
	targets = map[string]struct{ target vegeta.Target }{
		"queue-proxy-with-cc": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://queue-proxy-with-cc.default.svc.cluster.local?sleep=100",
			},
		},
		"activator-with-cc": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator-with-cc.default.svc.cluster.local?sleep=100",
			},
		},
		"activator-with-cc-lin": {
			target: vegeta.Target{
				Method: http.MethodGet,
				URL:    "http://activator-with-cc-lin.default.svc.cluster.local?sleep=100",
			},
		},
	}
)

const (
	namespace     = "default"
	benchmarkName = "Knative Serving rollout probe"
)

func main() {
	ctx := signals.NewContext()
	cfg := injection.ParseAndGetRESTConfigOrDie()
	ctx, startInformers := injection.EnableInjectionOrDie(ctx, cfg)
	startInformers()

	if *target == "" {
		log.Fatalf("-target is a required flag.")
	}

	// We cron quite often, so make sure that we don't severely overrun to
	// limit how noisy a neighbor we can be.
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

	// Send 1k QPS for the given duration with a 30s request timeout.
	rate := vegeta.Rate{Freq: 1000, Per: time.Second}
	targeter := vegeta.NewStaticTargeter(t.target)
	attacker := vegeta.NewAttacker(vegeta.Timeout(30 * time.Second))

	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: *target,
	})
	log.Print("Running rollout probe test with selector: ", selector)

	influxReporter, err := performance.NewInfluxReporter(map[string]string{"target": *target})
	if err != nil {
		log.Fatalf("failed to create influx reporter: %v", err.Error())
	}
	defer influxReporter.FlushAndShutdown()

	// Setup background metric processes
	deploymentStatus := performance.FetchDeploymentsStatus(ctx, namespace, selector, time.Second)
	routeStatus := performance.FetchRouteStatus(ctx, namespace, *target, time.Second)

	// Start the attack!
	results := attacker.Attack(targeter, rate, *duration, "rollout-test")
	firstRev, secondRev := "", ""

	// After a minute, update the Ksvc.
	updateSvc := time.After(30 * time.Second)

	metricResults := &vegeta.Metrics{}

LOOP:
	for {
		select {
		case <-ctx.Done():
			// If we time out or the pod gets shutdown via SIGTERM then start to
			// clean thing up.
			break LOOP

		case <-updateSvc:
			log.Println("Updating the service:", *target)
			sc := servingclient.Get(ctx)
			svc, err := sc.ServingV1().Services(namespace).Get(context.Background(), *target, metav1.GetOptions{})
			if err != nil {
				log.Fatalf("Error getting ksvc %s: %v", *target, err)
			}
			svc = svc.DeepCopy()

			// Update something irrelevant to trigger a rollout
			svc.Spec.Template.Annotations[autoscaling.MinScaleAnnotationKey] = "1"
			_, err = sc.ServingV1().Services(namespace).Update(context.Background(), svc, metav1.UpdateOptions{})
			if err != nil {
				log.Fatalf("Error updating ksvc %s: %v", *target, err)
			}
			log.Println("Successfully updated the service.")

		case res, ok := <-results:
			if ok {
				metricResults.Add(res)
			} else {
				// If there are no more results, then we're done!
				break LOOP
			}

		case ds := <-deploymentStatus:
			// Ignore deployment updates until we get current one.
			if firstRev == "" {
				continue
			}
			// Deployment name contains revision name.
			// If it is the first one -- report it.
			if strings.Contains(ds.DeploymentName, firstRev) {
				// Add a sample point for the deployment status.
				influxReporter.AddDataPoint(benchmarkName,
					map[string]interface{}{"desired-pods": ds.DesiredReplicas, "available-pods": ds.ReadyReplicas})
			} else if secondRev != "" && strings.Contains(ds.DeploymentName, secondRev) {
				// Otherwise report the pods for the new deployment.
				influxReporter.AddDataPoint(benchmarkName,
					map[string]interface{}{"desired-pods-new": ds.DesiredReplicas, "available-pods-new": ds.ReadyReplicas})
				// Ignore all other revisions' deployments if there are, since
				// they are from previous test run iterations, and we don't care about
				// their reported scale values (should be 0 & 100 depending on which
				// one it is).
			}

		case rs := <-routeStatus:
			if firstRev == "" {
				firstRev = rs.Traffic[0].RevisionName
				log.Println("Inferred first revision =", firstRev)
			}
			if len(rs.Traffic) != 1 {
				// If the name matches the first revision then it's before
				// we started the rollout. If not, then the rollout is
				// 100% complete.
				if secondRev == "" {
					secondRev = rs.Traffic[1].RevisionName
					log.Println("Inferred second revision =", secondRev)
				}
			}
		}
	}

	// Compute latency percentiles
	metricResults.Close()

	// Report the results
	influxReporter.AddDataPointsForMetrics(metricResults, benchmarkName)
	_ = vegeta.NewTextReporter(metricResults).Report(os.Stdout)

	if err := checkSLA(metricResults, rate); err != nil {
		// make sure to still write the stats
		influxReporter.FlushAndShutdown()
		log.Fatalf(err.Error())
	}

	log.Println("Load test finished")
}

func checkSLA(results *vegeta.Metrics, rate vegeta.ConstantPacer) error {
	// SLA 1: The p95 latency hitting a Knative Service
	// going through either JUST the queue-proxy or BOTH the activator and queue-proxy
	// falls in the +10ms range. Given that we sleep 100ms, the SLA is between 100-110ms.
	if results.Latencies.P95 >= 100*time.Millisecond && results.Latencies.P95 <= 110*time.Millisecond {
		log.Println("SLA 1 passed. P95 latency is in 100-110ms time range")
	} else {
		return fmt.Errorf("SLA 1 failed. P95 latency is not in 100-110ms time range: %s", results.Latencies.P95)
	}

	// SLA 2: making sure the defined vegeta rates is met
	if math.Round(results.Rate) == rate.Rate(time.Second) {
		log.Printf("SLA 2 passed. vegeta rate is %f", rate.Rate(time.Second))
	} else {
		return fmt.Errorf("SLA 2 failed. vegeta rate is %f, expected Rate is %f", results.Rate, rate.Rate(time.Second))
	}

	return nil
}
