//go:build e2e
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
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/system"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/networking"
	revnames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	testv1 "knative.dev/serving/test/v1"
)

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()
	for _, algo := range []string{
		autoscaling.MetricAggregationAlgorithmLinear,
		autoscaling.MetricAggregationAlgorithmWeightedExponential,
	} {
		algo := algo
		t.Run("aggregation-"+algo, func(t *testing.T) {
			t.Parallel()
			ctx := SetupSvc(t,
				&AutoscalerOptions{
					Class:             autoscaling.KPA,
					Metric:            autoscaling.Concurrency,
					Target:            containerConcurrency,
					TargetUtilization: targetUtilization,
				},
				test.Options{},
				rtesting.WithConfigAnnotations(map[string]string{
					autoscaling.MetricAggregationAlgorithmKey: algo,
				}))
			test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

			AssertAutoscaleUpToNumPods(ctx, 1, 2, time.After(60*time.Second), true /* quick */)
			assertScaleDown(ctx)
			AssertAutoscaleUpToNumPods(ctx, 0, 2, time.After(60*time.Second), true /* quick */)
		})
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

	ctx := SetupSvc(t,
		&AutoscalerOptions{
			Class:             class,
			Metric:            metric,
			Target:            target,
			TargetUtilization: targetUtilization,
		},
		test.Options{})

	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	ctx.t.Log("The autoscaler spins up additional replicas when traffic increases.")
	// Note: without the warm-up / gradual increase of load the test is
	// receiving 503 responses (overload) from the envoy.

	// Increase workload for 2 replicas for 90s. It takes longer on a weak
	// boskos cluster to propagate the state. See #10218.
	// Assert the number of expected replicas is between n-1 and n+1, where n is the # of desired replicas for 60s.
	// Assert the number of expected replicas is n and n+1 at the end of 90s, where n is the # of desired replicas.
	AssertAutoscaleUpToNumPods(ctx, 1, 2, time.After(90*time.Second), true /* quick */)
	// Increase workload scale to 3 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	AssertAutoscaleUpToNumPods(ctx, 2, 3, time.After(90*time.Second), true /* quick */)
	// Increase workload scale to 4 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	AssertAutoscaleUpToNumPods(ctx, 3, 4, time.After(90*time.Second), true /* quick */)
}

func TestAutoscaleSustaining(t *testing.T) {
	for _, algo := range []string{
		autoscaling.MetricAggregationAlgorithmLinear,
		autoscaling.MetricAggregationAlgorithmWeightedExponential,
	} {
		algo := algo
		t.Run("aggregation-"+algo, func(t *testing.T) {
			// When traffic increases, a knative app should scale up and sustain the scale
			// as long as the traffic sustains, despite whether it is switching modes between
			// normal and panic.

			ctx := SetupSvc(t,
				&AutoscalerOptions{
					Class:             autoscaling.KPA,
					Metric:            autoscaling.Concurrency,
					Target:            containerConcurrency,
					TargetUtilization: targetUtilization,
				},
				test.Options{},
				rtesting.WithConfigAnnotations(map[string]string{
					autoscaling.MetricAggregationAlgorithmKey: algo,
				}))
			test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

			AssertAutoscaleUpToNumPods(ctx, 1, 8, time.After(2*time.Minute), false /* quick */)
		})
	}
}

func TestTargetBurstCapacity(t *testing.T) {
	// This test sets up a service with CC=10 TU=70% and TBC=7.
	// Then sends requests at concurrency causing activator in the path.
	// Then at the higher concurrency 10,
	// getting spare capacity of 20-10=10, which should remove the
	// Activator from the request path.
	t.Parallel()

	ctx := SetupSvc(t,
		&AutoscalerOptions{
			Class:             autoscaling.KPA,
			Metric:            autoscaling.Concurrency,
			Target:            10,
			TargetUtilization: targetUtilization,
		},
		test.Options{},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey:                "7",
			autoscaling.PanicThresholdPercentageAnnotationKey: "200", // makes panicking rare
		}))
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	cm, err := ctx.clients.KubeClient.CoreV1().ConfigMaps(system.Namespace()).
		Get(context.Background(), netcfg.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Fail to get ConfigMap config-network:", err)
	}

	// TODO: Remove this when "activator always stay in path" is eliminated.
	dataplaneTrustMode := cm.Data[netcfg.DataplaneTrustKey]
	if (dataplaneTrustMode != "" && !strings.EqualFold(dataplaneTrustMode, string(netcfg.TrustDisabled))) || strings.EqualFold(cm.Data[netcfg.InternalEncryptionKey], "true") {
		t.Skip("Skipping TestTargetBurstCapacity as activator always stay in path.")
	}

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
		obsScale, _, err = numberOfReadyPods(ctx)
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
		svcEps, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingFlags.TestNamespace).Get(
			context.Background(), ctx.resources.Revision.Name, /* revision service name is equal to revision name*/
			metav1.GetOptions{})
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

	ctx := SetupSvc(t,
		&AutoscalerOptions{
			Class:             autoscaling.KPA,
			Metric:            autoscaling.Concurrency,
			Target:            10,
			TargetUtilization: targetUtilization,
		},
		test.Options{},
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

// Explicitly setting this should cause the revision to scale down after ~10s
func TestTargetBurstCapacityZero(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t,
		&AutoscalerOptions{
			Class:             autoscaling.KPA,
			Metric:            autoscaling.Concurrency,
			Target:            10,
			TargetUtilization: targetUtilization,
		},
		test.Options{},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "0",
			autoscaling.WindowAnnotationKey:    "10s", // scale faster
		}))

	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	deploymentName := revnames.Deployment(ctx.resources.Revision)

	t.Log("waiting for scale down")
	err := pkgtest.WaitForDeploymentState(
		context.Background(),
		ctx.Clients().KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == 0, nil
		},
		"DeploymentIsScaledDown",
		test.ServingFlags.TestNamespace,
		time.Minute,
	)

	if err != nil {
		t.Error(err)
	}
}

func TestFastScaleToZero(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t,
		&AutoscalerOptions{
			Class:             autoscaling.KPA,
			Metric:            autoscaling.Concurrency,
			Target:            containerConcurrency,
			TargetUtilization: targetUtilization,
		},
		test.Options{},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	cfg, err := autoscalerCM(ctx.clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}

	epsN := names.PrivateService(ctx.resources.Revision.Name)
	t.Logf("Waiting for emptying of %q ", epsN)

	// The first thing that happens when pods are starting to terminate
	// is that they stop being ready and endpoints controller removes them
	// from the ready set.
	// While pod termination itself can last quite some time (our pod termination
	// test allows for up to a minute). The 15s delay is based upon maximum
	// of 20 runs (11s) + 4s of buffer for reliability.
	st := time.Now()
	if err := wait.PollImmediate(1*time.Second, cfg.ScaleToZeroGracePeriod+15*time.Second, func() (bool, error) {
		eps, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingFlags.TestNamespace).Get(context.Background(), epsN, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return resources.ReadyAddressCount(eps) == 0, nil
	}); err != nil {
		t.Fatalf("Did not observe %q to actually be emptied", epsN)
	}

	t.Log("Total time to scale down:", time.Since(st))
}

const activationScale = 5

func TestActivationScale(t *testing.T) {
	t.Parallel()

	ctx := SetupSvc(t,
		&AutoscalerOptions{
			Class:             autoscaling.KPA,
			Metric:            autoscaling.Concurrency,
			Target:            6,
			TargetUtilization: 0.7},
		test.Options{},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.ActivationScaleKey: strconv.Itoa(activationScale),
		}))
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	clients := ctx.Clients()
	resources, err := testv1.GetResourceObjects(clients, *ctx.names)
	if err != nil {
		t.Errorf("error: unable to update resource: %s", err)
	}

	// initial scale of revision
	if err := wait.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {
		return *resources.Revision.Status.ActualReplicas > 0, nil
	}); err != nil {
		t.Errorf("error: revision never had active pods")
	}

	// scale to zero
	if err := wait.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {
		resources, _ = testv1.GetResourceObjects(clients, *ctx.names)
		return *resources.Revision.Status.ActualReplicas == 0, nil
	}); err != nil {
		t.Errorf("error: revision never scaled to zero")
	}

	url := ctx.resources.Route.Status.URL.URL()

	// send request, should scale up to activation scale
	if _, err = pkgtest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK),
		"ScalingFromZero",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Failed to scale up from zero %s: %v", url, err)
	}

	// wait for revision desired replicas to equal activation scale
	if err := wait.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {
		resources, _ = testv1.GetResourceObjects(clients, *ctx.names)
		return *resources.Revision.Status.DesiredReplicas == activationScale, nil
	}); err != nil {
		t.Errorf("error: desired pods never equal to activation scale")
	}
}
