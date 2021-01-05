// +build e2e

/*
Copyright 2018 The Knative Authors

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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
)

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization)
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	if err := AssertAutoscaleUpToNumPods(ctx, t.Logf, 1, 2, time.After(60*time.Second), true /* quick */); err != nil {
		t.Fatal(err)
	}
	if err := assertScaleDown(ctx, t.Logf); err != nil {
		t.Fatal(err)
	}
	if err := AssertAutoscaleUpToNumPods(ctx, t.Logf, 0, 2, time.After(60*time.Second), true /* quick */); err != nil {
		t.Fatal(err)
	}
}

func TestAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()
	runAutoscaleUpCountPods(t, autoscaling.KPA, autoscaling.Concurrency)
}

func TestRPSBasedAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()
	runAutoscaleUpCountPods(t, autoscaling.KPA, autoscaling.RPS)
}

// runAutoscaleUpCountPods is a test kernel to test the chosen autoscaler using the given
// metric tracks the given target.
func runAutoscaleUpCountPods(t *testing.T, class, metric string) {
	target := containerConcurrency
	if metric == autoscaling.RPS {
		target = rpsTarget
	}

	ctx := SetupSvc(t, class, metric, target, targetUtilization)
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
	// Note: without the warm-up / gradual increase of load the test is
	// receiving 503 responses (overload) from the envoy.

	// Increase workload for 2 replicas for 90s. It takes longer on a weak
	// boskos cluster to propagate the state. See #10218.
	// Assert the number of expected replicas is between n-1 and n+1, where n is the # of desired replicas for 60s.
	// Assert the number of expected replicas is n and n+1 at the end of 90s, where n is the # of desired replicas.
	if err := AssertAutoscaleUpToNumPods(ctx, t.Logf, 1, 2, time.After(90*time.Second), true /* quick */); err != nil {
		t.Fatal(err)
	}
	// Increase workload scale to 3 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	if err := AssertAutoscaleUpToNumPods(ctx, t.Logf, 2, 3, time.After(90*time.Second), true /* quick */); err != nil {
		t.Fatal(err)
	}
	// Increase workload scale to 4 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	if err := AssertAutoscaleUpToNumPods(ctx, t.Logf, 3, 4, time.After(90*time.Second), true /* quick */); err != nil {
		t.Fatal(err)
	}
}

func TestAutoscaleSustaining(t *testing.T) {
	// When traffic increases, a knative app should scale up and sustain the scale
	// as long as the traffic sustains, despite whether it is switching modes between
	// normal and panic.
	t.Parallel()

	ctx := SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization)
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	if err := AssertAutoscaleUpToNumPods(ctx, t.Logf, 1, 10, time.After(2*time.Minute), false /* quick */); err != nil {
		t.Fatal(err)
	}
}

func TestTargetBurstCapacity(t *testing.T) {
	// This test sets up a service with CC=10 TU=70% and TBC=7.
	// Then sends requests at concurrency causing activator in the path.
	// Then at the higher concurrency 10,
	// getting spare capacity of 20-10=10, which should remove the
	// Activator from the request path.
	t.Parallel()

	ctx := SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, 10 /* target concurrency*/, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey:                "7",
			autoscaling.PanicThresholdPercentageAnnotationKey: "200", // makes panicking rare
		}))
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}
	var (
		grp    errgroup.Group
		stopCh = make(chan struct{})
	)
	defer grp.Wait()
	defer close(stopCh)

	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, t.Logf, 7, stopCh)
	})

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}

	// Start second load generator.
	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, t.Logf, 5, stopCh)
	})

	// Wait for two stable pods.
	obsScale := 0.0
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		obsScale, err = numberOfReadyPods(ctx, t.Logf)
		if err != nil {
			return false, err
		}
		// We want exactly 2. Not 1, not panicking 3, just 2.
		return obsScale == 2, nil
	}); err != nil {
		t.Fatalf("Desired scale of 2 never achieved; last known value %v; err: %v", obsScale, err)
	}

	// Now read the service endpoints and make sure there are 2 endpoints there.
	// We poll, since network programming takes times, but the timeout is set for
	// uniformness with one above.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		svcEps, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingNamespace).Get(
			context.Background(), ctx.resources.Revision.Status.ServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		t.Log("resources.ReadyAddressCount(svcEps) =", resources.ReadyAddressCount(svcEps))
		return resources.ReadyAddressCount(svcEps) == 2, nil
	}); err != nil {
		t.Error("Never achieved subset of size 2:", err)
	}
}

func TestTargetBurstCapacityMinusOne(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, 10 /* target concurrency*/, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}))
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	_, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}
	aeps, err := ctx.clients.KubeClient.CoreV1().Endpoints(
		system.Namespace()).Get(context.Background(), networking.ActivatorServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Error getting activator endpoints:", err)
	}
	t.Log("Activator endpoints:", aeps)

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}
}

func TestFastScaleToZero(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}

	epsL, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s",
			serving.RevisionLabelKey, ctx.resources.Revision.Name,
			networking.ServiceTypeKey, networking.ServiceTypePrivate,
		),
	})
	if err != nil || len(epsL.Items) == 0 {
		t.Fatal("No endpoints or error:", err)
	}

	epsN := epsL.Items[0].Name
	t.Logf("Waiting for emptying of %q ", epsN)

	// The first thing that happens when pods are starting to terminate
	// is that they stop being ready and endpoints controller removes them
	// from the ready set.
	// While pod termination itself can last quite some time (our pod termination
	// test allows for up to a minute). The 15s delay is based upon maximum
	// of 20 runs (11s) + 4s of buffer for reliability.
	st := time.Now()
	if err := wait.PollImmediate(1*time.Second, cfg.ScaleToZeroGracePeriod+15*time.Second, func() (bool, error) {
		eps, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingNamespace).Get(context.Background(), epsN, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return resources.ReadyAddressCount(eps) == 0, nil
	}); err != nil {
		t.Fatalf("Did not observe %q to actually be emptied", epsN)
	}

	t.Log("Total time to scale down:", time.Since(st))
}
