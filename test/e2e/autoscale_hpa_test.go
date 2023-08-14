//go:build hpa
// +build hpa

/*
Copyright 2021 The Knative Authors

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
	"strconv"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	cpuTarget             = 75
	targetPods            = 5
	concurrency           = 10
	scaleUpTimeout        = 10 * time.Minute
	scaleToMinimumTimeout = 15 * time.Minute // 5 minutes is the default window for hpa to calculate if should scale down
	minPods               = 1.0
	maxPods               = 10.0
	primeNum              = 1000000
	memoryTarget          = 200 // 200Mib
	memoryTargetPods      = 2
	bloatNum              = 40

	vegetaParamPrime = "prime"
	vegetaParamBloat = "bloat"
)

func TestHPAAutoscaleUpDownUpMem(t *testing.T) {
	t.Skip("#11944: Skipped because of excessive flakiness")

	ctx := setupHPASvc(t, autoscaling.Memory, memoryTarget)
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())
	assertMemoryHPAAutoscaleUpToNumPods(ctx, memoryTargetPods, time.After(scaleUpTimeout), true /* quick */)
	assertScaleDownToOne(ctx)
	assertMemoryHPAAutoscaleUpToNumPods(ctx, memoryTargetPods, time.After(scaleUpTimeout), true /* quick */)
}

func TestHPAAutoscaleUpDownUp(t *testing.T) {
	ctx := setupHPASvc(t, autoscaling.CPU, cpuTarget)
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())
	assertCPUHPAAutoscaleUpToNumPods(ctx, targetPods, time.After(scaleUpTimeout), true /* quick */)
	assertScaleDownToOne(ctx)
	assertCPUHPAAutoscaleUpToNumPods(ctx, targetPods, time.After(scaleUpTimeout), true /* quick */)
}

func setupHPASvc(t *testing.T, metric string, target int) *TestContext {
	t.Helper()
	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names := &test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   autoscaleTestImageName,
	}
	resources, err := v1test.CreateServiceReady(t, clients, names,
		[]rtesting.ServiceOption{
			rtesting.WithConfigAnnotations(map[string]string{
				autoscaling.ClassAnnotationKey:    autoscaling.HPA,
				autoscaling.MetricAnnotationKey:   metric,
				autoscaling.TargetAnnotationKey:   strconv.Itoa(target),
				autoscaling.MaxScaleAnnotationKey: fmt.Sprintf("%d", int(maxPods)),
				autoscaling.WindowAnnotationKey:   "20s",
			}), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("300m"),
				},
			}),
		}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		spoof.MatchesAllOf(spoof.IsStatusOK),
		"CheckingEndpointAfterCreate",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
	}

	// Waiting until HPA status is available, as it takes some time until HPA starts collecting metrics.
	if err := waitForHPAState(t, resources.Revision.Name, resources.Revision.Namespace, clients); err != nil {
		t.Fatalf("Error collecting metrics by HPA: %v", err)
	}

	return &TestContext{
		t:         t,
		clients:   clients,
		names:     names,
		resources: resources,
		autoscaler: &AutoscalerOptions{
			Metric: metric,
			Target: target,
		},
	}
}

func assertCPUHPAAutoscaleUpToNumPods(ctx *TestContext, targetPods float64, done <-chan time.Time, quick bool) {
	ctx.t.Helper()

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		return generateTrafficAtFixedConcurrencyWithLoad(ctx, concurrency, vegetaParamPrime, primeNum, stopChan)
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, done, quick)
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

func assertMemoryHPAAutoscaleUpToNumPods(ctx *TestContext, targetPods float64, done <-chan time.Time, quick bool) {
	ctx.t.Helper()

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		return generateTrafficAtFixedConcurrencyWithLoad(ctx, concurrency, vegetaParamBloat, bloatNum, stopChan)
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, done, quick)
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

func generateTrafficAtFixedConcurrencyWithLoad(ctx *TestContext, concurrency int, vegetaParam string, vegetaValue int, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{} // Sends requests as quickly as possible, capped by MaxWorkers below.
	tlsConf := vegeta.DefaultTLSConfig
	if test.ServingFlags.HTTPS {
		tlsConf = test.TLSClientConfig(context.Background(), ctx.t.Logf, ctx.clients)
	}
	attacker := vegeta.NewAttacker(
		vegeta.Timeout(0), // No timeout is enforced at all.
		vegeta.Workers(uint64(concurrency)),
		vegeta.MaxWorkers(uint64(concurrency)),
		vegeta.TLSConfig(tlsConf))
	target, err := getVegetaTarget(
		ctx.clients.KubeClient, ctx.resources.Route.Status.URL.URL().Hostname(), pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain, vegetaParam, vegetaValue, test.ServingFlags.HTTPS)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %w", err)
	}

	ctx.t.Logf("Maintaining %d concurrent requests.", concurrency)
	return generateTraffic(ctx, attacker, pacer, stopChan, target)
}

func assertScaleDownToOne(ctx *TestContext) {
	deploymentName := resourcenames.Deployment(ctx.resources.Revision)
	if err := waitForScaleToOne(ctx.t, deploymentName, ctx.clients); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}
	ctx.t.Logf("Wait for all pods to terminate.")

	if err := pkgTest.WaitForPodListState(
		context.Background(),
		ctx.clients.KubeClient,
		func(p *corev1.PodList) (bool, error) {
			if !(len(getDepPods(p.Items, deploymentName)) == 1) {
				return false, nil
			}
			return true, nil
		},
		"WaitForAvailablePods", test.ServingFlags.TestNamespace); err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", deploymentName, err)
	}

	ctx.t.Logf("The Revision should remain ready after scaling to one.")
	if err := v1test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, v1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to one: %v", ctx.names.Revision, err)
	}

	ctx.t.Logf("Scaled down.")
}

func getDepPods(nsPods []corev1.Pod, deploymentName string) []corev1.Pod {
	var pods []corev1.Pod
	for _, p := range nsPods {
		if strings.Contains(p.Name, deploymentName) && !strings.Contains(p.Status.Reason, "Evicted") {
			pods = append(pods, p)
		}
	}
	return pods
}

func waitForScaleToOne(t *testing.T, deploymentName string, clients *test.Clients) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to one", deploymentName)

	return pkgTest.WaitForDeploymentState(
		context.Background(),
		clients.KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == 1, nil
		},
		"DeploymentIsScaledDown",
		test.ServingFlags.TestNamespace,
		scaleToMinimumTimeout,
	)
}

func waitForHPAState(t *testing.T, name, namespace string, clients *test.Clients) error {
	return wait.PollImmediate(time.Second, 15*time.Minute, func() (bool, error) {
		hpa, err := clients.KubeClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if hpa.Status.CurrentMetrics == nil {
			t.Logf("Waiting for hpa.status is available: %#v", hpa.Status)
			return false, nil
		}
		return true, nil
	})
}
