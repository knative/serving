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
	"os"
	"path/filepath"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/signals"
	"sigs.k8s.io/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"

	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	networkingclient "knative.dev/networking/pkg/client/injection/client"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	"knative.dev/serving/test/performance/performance"
)

const (
	benchmarkName = "Knative Serving reconciliation delay"
	template      = "knative-service-template.yaml"
)

var (
	duration  = flag.Duration("duration", 1*time.Minute, "The duration of the benchmark to run.")
	frequency = flag.Duration("frequency", 5*time.Second, "The frequency at which to create services.")
)

func main() {
	ctx := signals.NewContext()
	cfg := injection.ParseAndGetRESTConfigOrDie()
	ctx, startInformers := injection.EnableInjectionOrDie(ctx, cfg)
	startInformers()

	tmpl, err := readTemplate()
	if err != nil {
		log.Fatalf("Unable to read template %s: %v", template, err)
	}

	ctx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()

	sc := servingclient.Get(ctx)
	cleanupServices := func() error {
		return sc.ServingV1().Services(tmpl.Namespace).DeleteCollection(
			context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{})
	}
	defer cleanupServices()

	// Wrap fatalf to make sure we clean up our created resources
	fatalf := func(f string, args ...interface{}) {
		cleanupServices()
		log.Fatalf(f, args...)
	}

	if err := cleanupServices(); err != nil {
		fatalf("Error cleaning up services: %v", err)
	}

	lo := metav1.ListOptions{TimeoutSeconds: ptr.Int64(int64(duration.Seconds()))}

	serviceWI, err := sc.ServingV1().Services(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch services: %v", err)
	}
	defer serviceWI.Stop()
	serviceSeen := sets.Set[string]{}

	configurationWI, err := sc.ServingV1().Configurations(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch configurations: %v", err)
	}
	defer configurationWI.Stop()
	configurationSeen := sets.Set[string]{}

	routeWI, err := sc.ServingV1().Routes(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch routes: %v", err)
	}
	defer routeWI.Stop()
	routeSeen := sets.Set[string]{}

	revisionWI, err := sc.ServingV1().Revisions(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch revisions: %v", err)
	}
	defer revisionWI.Stop()
	revisionSeen := sets.Set[string]{}

	nc := networkingclient.Get(ctx)
	ingressWI, err := nc.NetworkingV1alpha1().Ingresses(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch ingresss: %v", err)
	}
	defer ingressWI.Stop()
	ingressSeen := sets.Set[string]{}

	sksWI, err := nc.NetworkingV1alpha1().ServerlessServices(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch skss: %v", err)
	}
	defer sksWI.Stop()
	sksSeen := sets.Set[string]{}

	paWI, err := sc.AutoscalingV1alpha1().PodAutoscalers(tmpl.Namespace).Watch(ctx, lo)
	if err != nil {
		fatalf("Unable to watch pas: %v", err)
	}
	defer paWI.Stop()
	paSeen := sets.Set[string]{}

	tick := time.NewTicker(*frequency)
	metricResults := func() *vegeta.Metrics {
		influxReporter, err := performance.NewInfluxReporter(map[string]string{})
		if err != nil {
			fatalf(fmt.Sprintf("failed to create influx reporter: %v", err.Error()))
		}
		defer influxReporter.FlushAndShutdown()

		// We use vegeta.Metrics here as a metrics collector because it already contains logic to calculate percentiles
		mr := &vegeta.Metrics{}
		for {
			select {
			case <-ctx.Done():
				// If we time out or the pod gets shutdown via SIGTERM then start to clean thing up.
				// Compute latency percentiles
				mr.Close()
				return mr

			case <-tick.C:
				_, err := sc.ServingV1().Services(tmpl.Namespace).Create(ctx, tmpl, metav1.CreateOptions{})
				if err != nil {
					log.Println("Error creating service:", err)
					break
				}

			case event := <-serviceWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				svc := event.Object.(*v1.Service)
				handleEvent(influxReporter, mr, svc, svc.Status.Status, serviceSeen, "Service")

			case event := <-configurationWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				cfg := event.Object.(*v1.Configuration)
				handleEvent(influxReporter, mr, cfg, cfg.Status.Status, configurationSeen, "Configuration")

			case event := <-routeWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				rt := event.Object.(*v1.Route)
				handleEvent(influxReporter, mr, rt, rt.Status.Status, routeSeen, "Route")

			case event := <-revisionWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				rev := event.Object.(*v1.Revision)
				handleEvent(influxReporter, mr, rev, rev.Status.Status, revisionSeen, "Revision")

			case event := <-ingressWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				ing := event.Object.(*netv1alpha1.Ingress)
				handleEvent(influxReporter, mr, ing, ing.Status.Status, ingressSeen, "Ingress")

			case event := <-sksWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				ing := event.Object.(*netv1alpha1.ServerlessService)
				handleEvent(influxReporter, mr, ing, ing.Status.Status, sksSeen, "ServerlessService")

			case event := <-paWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				pa := event.Object.(*autoscalingv1alpha1.PodAutoscaler)
				handleEvent(influxReporter, mr, pa, pa.Status.Status, paSeen, "PodAutoscaler")
			}
		}
	}()

	// Report to stdout
	_ = vegeta.NewTextReporter(metricResults).Report(os.Stdout)

	expectedServices := duration.Seconds() / frequency.Seconds()
	if err := checkSLA(metricResults, expectedServices); err != nil {
		fatalf(err.Error())
	}

	log.Println("Reconciliation delay run finished")
}

func handleEvent(influxReporter *performance.InfluxReporter, metricResults *vegeta.Metrics, svc kmeta.Accessor,
	status duckv1.Status, seen sets.Set[string], metric string) {
	if seen.Has(svc.GetName()) {
		return
	}

	cc := status.GetCondition(apis.ConditionReady)
	if cc == nil || cc.Status == corev1.ConditionUnknown {
		return
	}

	seen.Insert(svc.GetName())
	created := svc.GetCreationTimestamp().Time
	ready := cc.LastTransitionTime.Inner.Time
	elapsed := ready.Sub(created)

	if cc.Status == corev1.ConditionTrue {
		influxReporter.AddDataPoint(benchmarkName, map[string]interface{}{metric: elapsed.Seconds()})
		result := vegeta.Result{
			Latency: elapsed,
		}
		// We need to count ready Services separately, for the SLA
		if metric == "Service" {
			result.Code = 200
			log.Printf("Service %s ready in %vs", svc.GetName(), elapsed.Seconds())
		}
		metricResults.Add(&result)
	} else if cc.Status == corev1.ConditionFalse {
		log.Printf("Not Ready: %s; %s: %s", svc.GetName(), cc.Reason, cc.Message)
	}
}

func readTemplate() (*v1.Service, error) {
	path := filepath.Join(os.Getenv("KO_DATA_PATH"), template)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	svc := &v1.Service{}
	if err := yaml.Unmarshal(b, svc); err != nil {
		return nil, err
	}

	// only set ownerReferences when deployed to a cluster
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		svc.OwnerReferences = []metav1.OwnerReference{{
			APIVersion:         "v1",
			Kind:               "Pod",
			Name:               podName,
			UID:                types.UID(os.Getenv("POD_UID")),
			Controller:         ptr.Bool(true),
			BlockOwnerDeletion: ptr.Bool(true),
		}}
	}

	return svc, nil
}

func checkSLA(results *vegeta.Metrics, expectedReadyServices float64) error {
	// SLA 1: The number of services deployed to "Ready=True" should be reached.
	// Example: Configured to run for 35m with a frequency of 5s, the theoretical limit is 420
	// if deployments take 0s. Factoring in deployment latency, we will miss a
	// handful of the trailing deployments, so we relax this a bit to 97% of that.
	relaxedExpectedReadyServices := math.Floor(expectedReadyServices * 0.97)

	// Success is a percentage of all requests, so we need to multiply this by the total requests
	readyServices := results.Success * float64(results.Requests)
	if readyServices >= relaxedExpectedReadyServices && readyServices <= expectedReadyServices {
		log.Printf("SLA 1 passed. Amount of ready services is within the expected range. Is: %f, expected: %f-%f",
			readyServices, relaxedExpectedReadyServices, expectedReadyServices)
	} else {
		return fmt.Errorf("SLA 1 failed. Amount of ready services is out of the expected range. Is: %f, Range: %f-%f",
			readyServices, relaxedExpectedReadyServices, expectedReadyServices)
	}

	// SLA 2: The p95 latency deploying a new service takes up to max 25 seconds.
	if results.Latencies.P95 >= 0*time.Second && results.Latencies.P95 <= 25*time.Second {
		log.Println("SLA 2 passed. P95 latency is in 0-25s time range")
	} else {
		return fmt.Errorf("SLA 2 failed. P95 latency is not in 0-25s time range: %s", results.Latencies.P95)
	}

	return nil
}
