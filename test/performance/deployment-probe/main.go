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
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ghodss/yaml"
	"github.com/google/mako/go/quickstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/signals"

	"knative.dev/pkg/test/mako"
	asv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
)

var (
	template  = flag.String("template", "", "The service template to load from kodata/")
	duration  = flag.Duration("duration", 25*time.Minute, "The duration of the benchmark to run.")
	frequency = flag.Duration("frequency", 5*time.Second, "The frequency at which to create services.")
)

func readTemplate() (*v1beta1.Service, error) {
	path := filepath.Join(os.Getenv("KO_DATA_PATH"), *template+"-template.yaml")
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	svc := &v1beta1.Service{}
	if err := yaml.Unmarshal([]byte(b), svc); err != nil {
		return nil, err
	}

	svc.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         "v1",
		Kind:               "Pod",
		Name:               os.Getenv("POD_NAME"),
		UID:                types.UID(os.Getenv("POD_UID")),
		Controller:         ptr.Bool(true),
		BlockOwnerDeletion: ptr.Bool(true),
	}}

	return svc, nil
}

func handle(q *quickstore.Quickstore, svc kmeta.Accessor, status duckv1.Status,
	seen *sets.String, metric string) {
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
		q.AddSamplePoint(mako.XTime(created), map[string]float64{
			metric: elapsed.Seconds(),
		})
		log.Printf("Ready: %s", svc.GetName())
	} else if cc.Status == corev1.ConditionFalse {
		q.AddError(mako.XTime(created), cc.Message)
		log.Printf("Not Ready: %s; %s: %s", svc.GetName(), cc.Reason, cc.Message)
	}
}

func main() {
	flag.Parse()

	// We want this for properly handling Kubernetes container lifecycle events.
	ctx := signals.NewContext()

	tmpl, err := readTemplate()
	if err != nil {
		log.Fatalf("Unable to read template %s: %v", *template, err)
	}

	// We cron every 30 minutes, so make sure that we don't severely overrun to
	// limit how noisy a neighbor we can be.
	ctx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()

	// Tag this run with the various flag values.
	tags := []string{
		"template=" + *template,
		"duration=" + duration.String(),
		"frequency=" + frequency.String(),
	}
	mc, err := mako.Setup(ctx, tags...)
	if err != nil {
		log.Fatalf("Failed to setup mako: %v", err)
	}
	q, qclose, ctx := mc.Quickstore, mc.ShutDownFunc, mc.Context
	// Use a fresh context here so that our RPC to terminate the sidecar
	// isn't subject to our timeout (or we won't shut it down when we time out)
	defer qclose(context.Background())

	sc := servingclient.Get(ctx)
	cleanup := func() error {
		return sc.ServingV1beta1().Services(tmpl.Namespace).DeleteCollection(
			&metav1.DeleteOptions{}, metav1.ListOptions{})
	}
	defer cleanup()

	// Wrap fatalf in a helper or our sidecar will live forever.
	fatalf := func(f string, args ...interface{}) {
		qclose(context.Background())
		cleanup()
		log.Fatalf(f, args...)
	}

	// Set up the threshold analyzers for the selected benchmark.  This will
	// cause Mako/Quickstore to analyze the results we are storing and flag
	// things that are outside of expected bounds.
	q.Input.ThresholdInputs = append(q.Input.ThresholdInputs,
		newDeploy95PercentileLatency(tags...),
		newReadyDeploymentCount(tags...),
	)

	if err := cleanup(); err != nil {
		fatalf("Error cleaning up services: %v", err)
	}

	lo := metav1.ListOptions{TimeoutSeconds: ptr.Int64(int64(duration.Seconds()))}

	// TODO(mattmoor): We could maybe use a duckv1.KResource to eliminate this boilerplate.

	serviceWI, err := sc.ServingV1beta1().Services(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch services: %v", err)
	}
	defer serviceWI.Stop()
	serviceSeen := sets.String{}

	configurationWI, err := sc.ServingV1beta1().Configurations(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch configurations: %v", err)
	}
	defer configurationWI.Stop()
	configurationSeen := sets.String{}

	routeWI, err := sc.ServingV1beta1().Routes(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch routes: %v", err)
	}
	defer routeWI.Stop()
	routeSeen := sets.String{}

	revisionWI, err := sc.ServingV1beta1().Revisions(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch revisions: %v", err)
	}
	defer revisionWI.Stop()
	revisionSeen := sets.String{}

	ingressWI, err := sc.NetworkingV1alpha1().Ingresses(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch ingresss: %v", err)
	}
	defer ingressWI.Stop()
	ingressSeen := sets.String{}

	sksWI, err := sc.NetworkingV1alpha1().ServerlessServices(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch skss: %v", err)
	}
	defer sksWI.Stop()
	sksSeen := sets.String{}

	paWI, err := sc.AutoscalingV1alpha1().PodAutoscalers(tmpl.Namespace).Watch(lo)
	if err != nil {
		fatalf("Unable to watch pas: %v", err)
	}
	defer paWI.Stop()
	paSeen := sets.String{}

	tick := time.NewTicker(*frequency)
	func() {
		for {
			select {
			case <-ctx.Done():
				// If we timeout or the pod gets shutdown via SIGTERM then start to
				// clean thing up.
				return

			case ts := <-tick.C:
				svc, err := sc.ServingV1beta1().Services(tmpl.Namespace).Create(tmpl)
				if err != nil {
					q.AddError(mako.XTime(ts), err.Error())
					log.Printf("Error creating service: %v", err)
					break
				}
				log.Printf("Created: %s", svc.Name)

			case event := <-serviceWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				svc := event.Object.(*v1beta1.Service)
				handle(q, svc, svc.Status.Status, &serviceSeen, "dl")

			case event := <-configurationWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				cfg := event.Object.(*v1beta1.Configuration)
				handle(q, cfg, cfg.Status.Status, &configurationSeen, "cl")

			case event := <-routeWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				rt := event.Object.(*v1beta1.Route)
				handle(q, rt, rt.Status.Status, &routeSeen, "rl")

			case event := <-revisionWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				rev := event.Object.(*v1beta1.Revision)
				handle(q, rev, rev.Status.Status, &revisionSeen, "rvl")

			case event := <-ingressWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				ing := event.Object.(*netv1alpha1.Ingress)
				handle(q, ing, ing.Status.Status, &ingressSeen, "il")

			case event := <-sksWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				ing := event.Object.(*netv1alpha1.ServerlessService)
				handle(q, ing, ing.Status.Status, &sksSeen, "sksl")

			case event := <-paWI.ResultChan():
				if event.Type != watch.Modified {
					// Skip events other than modifications
					break
				}
				pa := event.Object.(*asv1alpha1.PodAutoscaler)
				handle(q, pa, pa.Status.Status, &paSeen, "pal")
			}
		}
	}()

	// Commit this benchmark run to Mako!
	out, err := q.Store()
	if err != nil {
		fatalf("q.Store error: %v: %v", out, err)
	}
	log.Printf("Done! Run: %s\n", out.GetRunChartLink())
}
