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
	"fmt"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/system"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
)

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization, autoscaleTestImageName, validateEndpoint)
	defer test.TearDown(ctx.clients, ctx.names)

	assertAutoscaleUpToNumPods(ctx, 1, 2, 60*time.Second, true)
	assertScaleDown(ctx)
	assertAutoscaleUpToNumPods(ctx, 0, 2, 60*time.Second, true)
}

func TestAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	RunAutoscaleUpCountPods(t, autoscaling.KPA)
}

func TestRPSBasedAutoscaleUpCountPods(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	RunTestRPSBasedAutoscaleUpCountPods(t, autoscaling.KPA)
}

func TestAutoscaleSustaining(t *testing.T) {
	// When traffic increases, a knative app should scale up and sustain the scale
	// as long as the traffic sustains, despite whether it is switching modes between
	// normal and panic.
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization, autoscaleTestImageName, validateEndpoint)
	defer test.TearDown(ctx.clients, ctx.names)

	assertAutoscaleUpToNumPods(ctx, 1, 10, 2*time.Minute, false)
}

func TestTargetBurstCapacity(t *testing.T) {
	// This test sets up a service with CC=10 TU=70% and TBC=7.
	// Then sends requests at concurrency causing activator in the path.
	// Then at the higher concurrency 10,
	// getting spare capacity of 20-10=10, which should remove the
	// Activator from the request path.
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, 10 /* target concurrency*/, targetUtilization, autoscaleTestImageName, validateEndpoint,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey:                "7",
			autoscaling.PanicThresholdPercentageAnnotationKey: "200", // makes panicking rare
		}))
	defer test.TearDown(ctx.clients, ctx.names)

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

	// We'll terminate the test via stopCh.
	const duration = time.Hour

	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, 7, duration, stopCh)
	})

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx.resources, ctx.clients); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}

	// Start second load generator.
	grp.Go(func() error {
		return generateTrafficAtFixedConcurrency(ctx, 5, duration, stopCh)
	})

	// Wait for two stable pods.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		x, err := numberOfReadyPods(ctx)
		if err != nil {
			return false, err
		}
		// We want exactly 2. Not 1, not panicing 3, just 2.
		return x == 2, nil
	}); err != nil {
		t.Fatal("Desired scale of 2 never achieved:", err)
	}

	// Now read the service endpoints and make sure there are 2 endpoints there.
	// We poll, since network programming takes times, but the timeout is set for
	// uniformness with one above.
	if err := wait.Poll(250*time.Millisecond, 2*cfg.StableWindow, func() (bool, error) {
		svcEps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(
			ctx.resources.Revision.Status.ServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		t.Logf("resources.ReadyAddressCount(svcEps) = %d", resources.ReadyAddressCount(svcEps))
		return resources.ReadyAddressCount(svcEps) == 2, nil
	}); err != nil {
		t.Errorf("Never achieved subset of size 2: %v", err)
	}
}

func TestTargetBurstCapacityMinusOne(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, 10 /* target concurrency*/, targetUtilization, autoscaleTestImageName, validateEndpoint,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}))
	defer test.TearDown(ctx.clients, ctx.names)

	_, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}
	aeps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(
		system.Namespace()).Get(networking.ActivatorServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Error getting activator endpoints:", err)
	}
	t.Log("Activator endpoints:", aeps)

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(ctx.resources, ctx.clients); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}
}

func TestFastScaleToZero(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization, autoscaleTestImageName, validateEndpoint,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	defer test.TearDown(ctx.clients, ctx.names)

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}

	epsL, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).List(metav1.ListOptions{
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

	// The first thing that happens when pods are starting to terminate,
	// if that they stop being ready and endpoints controller removes them
	// from the ready set.
	// While pod termination itself can last quite some time (our pod termination
	// test allows for up to a minute). The 15s delay is based upon maximum
	// of 20 runs (11s) + 4s of buffer for reliability.
	st := time.Now()
	if err := wait.PollImmediate(1*time.Second, cfg.ScaleToZeroGracePeriod+15*time.Second, func() (bool, error) {
		eps, err := ctx.clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(epsN, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return resources.ReadyAddressCount(eps) == 0, nil
	}); err != nil {
		t.Fatalf("Did not observe %q to actually be emptied", epsN)
	}

	t.Logf("Total time to scale down: %v", time.Since(st))
}

func TestGracefulScaledown(t *testing.T) {
	t.Skip()
	cancel := logstream.Start(t)
	defer cancel()

	ctx := setup(t, autoscaling.KPA, autoscaling.Concurrency, 1 /* target */, 1, /* targetUtilization */
		wsHostnameTestImageName, nil, /* no validation */
		rtesting.WithContainerConcurrency(1),
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}))
	defer test.TearDown(ctx.clients, ctx.names)

	autoscalerConfigMap, err := rawCM(ctx.clients, autoscalerconfig.ConfigName)
	if err != nil {
		t.Errorf("Error retrieving autoscaler configmap: %v", err)
	}

	patchedAutoscalerConfigMap := autoscalerConfigMap.DeepCopy()
	patchedAutoscalerConfigMap.Data["enable-graceful-scaledown"] = "true"
	patchCM(ctx.clients, patchedAutoscalerConfigMap)
	defer patchCM(ctx.clients, autoscalerConfigMap)
	test.CleanupOnInterrupt(func() { patchCM(ctx.clients, autoscalerConfigMap) })

	if err = assertGracefulScaledown(t, ctx, 4 /* desired pods */); err != nil {
		t.Errorf("Failed: %v", err)
	}
}

func assertGracefulScaledown(t *testing.T, ctx *testContext, size int) error {
	// start x running pods; x == size
	hostConnMap, err := uniqueHostConnections(t, ctx.names, size)
	if err != nil {
		return err
	}

	// only keep openConnCount connections open for the test
	openConnCount := size / 2
	deleteHostConnections(hostConnMap, size-openConnCount)

	defer deleteHostConnections(hostConnMap, openConnCount)

	timer := time.NewTicker(2 * time.Second)
	for range timer.C {
		readyCount, err := numberOfReadyPods(ctx)
		if err != nil {
			return err
		}

		if int(readyCount) < openConnCount {
			return fmt.Errorf("failed keeping the right number of pods. Ready(%d) != Expected(%d)", int(readyCount), openConnCount)
		}

		if int(readyCount) == openConnCount {
			pods, err := allPods(ctx)
			if err != nil {
				return err
			}

			for _, p := range pods {
				if p.Status.Phase != corev1.PodRunning || p.DeletionTimestamp != nil {
					continue
				}

				if _, ok := hostConnMap.Load(p.Name); !ok {
					return fmt.Errorf("failed by keeping the wrong pod %s", p.Name)
				}
			}
			break
		}
	}

	return nil
}
