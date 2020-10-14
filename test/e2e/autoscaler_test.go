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
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

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

	cfg, err := AutoscalerCM(ctx.clients)
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
		return generateTrafficAtFixedConcurrency(ctx, 7, stopCh)
	})

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}

	// Start second load generator.
	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, 5, stopCh)
	})

	// Wait for two stable pods.
	obsScale := 0.0
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		obsScale, err = numberOfReadyPods(ctx)
		if err != nil {
			return false, err
		}
		// We want exactly 2. Not 1, not panicing 3, just 2.
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

func TestScaleFromZero(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	AssertScaleDown(ctx)

	url := ctx.resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		ctx.clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK)),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, ctx.clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't return 200: %v", url, ctx.names.Route, err)
	}
}
