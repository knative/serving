/*
Copyright 2020 The Knative Authors

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
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	servingclient "knative.dev/serving/pkg/client/injection/client"

	"knative.dev/pkg/signals"
	"knative.dev/pkg/test/mako"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test/performance"
	"knative.dev/serving/test/performance/metrics"
)

var (
	target   = flag.String("target", "", "The target to attack.")
	duration = flag.Duration("duration", 5*time.Minute, "The duration of the probe")
)

const namespace = "default"

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
		log.Fatal("Failed to setup mako: ", err)
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

	// Send 1k QPS for the given duration with a 30s request timeout.
	rate := vegeta.Rate{Freq: 3600, Per: time.Second}
	targeter := vegeta.NewStaticTargeter(t.target)
	attacker := vegeta.NewAttacker(vegeta.Timeout(30 * time.Second))

	// Create a new aggregateResult to accumulate the results.
	ar := metrics.NewAggregateResult(int(duration.Seconds()))

	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: *target,
	})
	log.Print("Selector: ", selector)

	// Setup background metric processes
	deploymentStatus := metrics.FetchDeploymentsStatus(ctx, namespace, selector, time.Second)
	routeStatus := metrics.FetchRouteStatus(ctx, namespace, *target, time.Second)

	// Start the attack!
	results := attacker.Attack(targeter, rate, *duration, "rollout-test")
	firstRev, secondRev := "", ""

	// After a minute, update the Ksvc.
	updateSvc := time.After(30 * time.Second)

	// Since we might qfatal in the end, this would not execute the deferred calls
	// thus failing the restore. So bind and execute explicitly.
	var restoreFn func()
LOOP:
	for {
		select {
		case <-ctx.Done():
			// If we timeout or the pod gets shutdown via SIGTERM then start to
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
			// Make sure we start with a single instance.

			// At the end of the benchmark, restore to the previous value.
			if prev := svc.Spec.Template.Annotations["autoscaling.knative.dev/minScale"]; prev != "" {
				restoreFn = func() {
					restore, err := sc.ServingV1().Services(namespace).Get(context.Background(), *target, metav1.GetOptions{})
					if err != nil {
						log.Println("Error getting service", err)
						return
					}
					restore = restore.DeepCopy()
					restore.Spec.Template.Annotations["autoscaling.knative.dev/minScale"] = prev
					_, err = sc.ServingV1().Services(namespace).Update(
						context.Background(), restore, metav1.UpdateOptions{})
					log.Printf("Restoring the service to initial minScale = %s, err: %#v", prev, err)
					// Also remove the oldest revision, to keep the list of revisions short.
					sc.ServingV1().Revisions(namespace).Delete(ctx, restore.Status.LatestReadyRevisionName,
						metav1.DeleteOptions{})
				}
			}
			svc.Spec.Template.Annotations["autoscaling.knative.dev/minScale"] = "1"
			_, err = sc.ServingV1().Services(namespace).Update(context.Background(), svc, metav1.UpdateOptions{})
			if err != nil {
				fatalf("Error updating ksvc %s: %v", *target, err)
			}
			log.Println("Successfully updated the service.")
		case res, ok := <-results:
			if !ok {
				// Once we have read all of the request results, break out of
				// our loop.
				break LOOP
			}
			// Handle the result for this request.
			metrics.HandleResult(q, *res, t.stat, ar)
		case ds := <-deploymentStatus:
			// Ignore deployment updates until we get current one.
			if firstRev == "" {
				break LOOP
			}
			// Deployment name contains revision name.
			// If it is the first one -- report it.
			if strings.Contains(ds.DeploymentName, firstRev) {
				// Add a sample point for the deployment status.
				q.AddSamplePoint(mako.XTime(ds.Time), map[string]float64{
					"dp": float64(ds.DesiredReplicas),
					"ap": float64(ds.ReadyReplicas),
				})
			} else if secondRev != "" && strings.Contains(ds.DeploymentName, secondRev) {
				// Otherwise eport the pods for the new deployment.
				q.AddSamplePoint(mako.XTime(ds.Time), map[string]float64{
					"dp2": float64(ds.DesiredReplicas),
					"ap2": float64(ds.ReadyReplicas),
				})
				// Ignore all other revisions' deployments if there are, since
				// they are from previous test run iterations and we don't care about
				// their reported scale values (should be 0 & 100 depending on which
				// one it is).
			}
		case rs := <-routeStatus:
			if firstRev == "" {
				firstRev = rs.Traffic[0].RevisionName
				log.Println("Inferred first revision =", firstRev)
			}
			v := make(map[string]float64, 2)
			if len(rs.Traffic) == 1 {
				// If the name matches the first revision then it's before
				// we started the rollout. If not, then the rollout is
				// 100% complete.
				if rs.Traffic[0].RevisionName == firstRev {
					v["t1"] = float64(*rs.Traffic[0].Percent)
				} else {
					v["t2"] = float64(*rs.Traffic[0].Percent)
				}
			} else {
				v["t1"] = float64(*rs.Traffic[0].Percent)
				v["t2"] = float64(*rs.Traffic[1].Percent)
				if secondRev == "" {
					secondRev = rs.Traffic[1].RevisionName
					log.Println("Inferred second revision =", secondRev)
				}
			}
			q.AddSamplePoint(mako.XTime(rs.Time), v)
		}
	}
	if restoreFn != nil {
		restoreFn()
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
