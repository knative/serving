// +build e2ehpa
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
	"knative.dev/pkg/test/spoof"
	"strconv"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	cpuTarget             = 75
	targetPods            = 5
	scaleUpTimeout        = 3 * time.Minute
	scaleToMinimumTimeout = 10 * time.Minute // 5 minutes is the default window for hpa to calculate if should scale down
	minimumReplicas       = 1
	minPods               = 1.0
	maxPods               = 10.0
	primeNum              = 500000
)

func TestHPAAutoscaleUpDownUp(t *testing.T) {
	ctx := setupHPASvc(t, autoscaling.HPA, autoscaling.CPU, cpuTarget)
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())
	assertHPAAutoscaleUpToNumPods(ctx, minimumReplicas, targetPods, time.After(scaleUpTimeout), true /* quick */)
	assertScaleDownToOne(ctx)
	assertHPAAutoscaleUpToNumPods(ctx, minimumReplicas, targetPods, time.After(scaleUpTimeout), true /* quick */)
}

func setupHPASvc(t *testing.T, class, metric string, target int) *TestContext {
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
				autoscaling.ClassAnnotationKey:    class,
				autoscaling.MetricAnnotationKey:   metric,
				autoscaling.TargetAnnotationKey:   strconv.Itoa(target),
				autoscaling.MinScaleAnnotationKey: fmt.Sprintf("%d", minimumReplicas),
				autoscaling.MaxScaleAnnotationKey: fmt.Sprintf("%d", int(maxPods)),
			}), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
			}),
		}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK)),
		"CheckingEndpointAfterCreate",
		false,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
	}

	return &TestContext{
		t:           t,
		logf:        t.Logf,
		clients:     clients,
		names:       names,
		resources:   resources,
		targetValue: target,
		metric:      metric,
	}
}
func assertHPAAutoscaleUpToNumPods(ctx *TestContext, curPods, targetPods float64, done <-chan time.Time, quick bool) {
	ctx.t.Helper()
	wait := autoscaleHPAUpToNumPods(ctx, curPods, targetPods, done, quick)
	if err := wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

func autoscaleHPAUpToNumPods(ctx *TestContext, curPods, targetPods float64, done <-chan time.Time, quick bool) func() error {
	ctx.t.Helper()

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		return generateTrafficAtFixedRPSWithCPULoad(ctx, int(targetPods*5), stopChan)
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, done, quick)
	})

	return grp.Wait
}

func generateTrafficAtFixedRPSWithCPULoad(ctx *TestContext, rps int, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{Freq: rps, Per: time.Second}
	attacker := vegeta.NewAttacker(vegeta.Timeout(0)) // No timeout is enforced at all.
	target, err := getVegetaTarget(
		ctx.clients.KubeClient, ctx.resources.Route.Status.URL.URL().Hostname(), pkgTest.Flags.IngressEndpoint, test.ServingFlags.ResolvableDomain, "prime", primeNum)
	if err != nil {
		return fmt.Errorf("error creating vegeta target: %w", err)
	}
	ctx.logf("Maintaining %v RPS.", rps)
	return generateTraffic(ctx, attacker, pacer, stopChan, target)
}

func assertScaleDownToOne(ctx *TestContext) {
	deploymentName := resourcenames.Deployment(ctx.resources.Revision)
	if err := waitForScaleToOne(ctx.t, deploymentName, ctx.clients); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}
	// Account for the case where scaling up uses all available pods.
	ctx.logf("Wait for all pods to terminate.")

	if err := pkgTest.WaitForPodListState(
		context.Background(),
		ctx.clients.KubeClient,
		func(p *corev1.PodList) (bool, error) {
			if !(len(getDepPods(p.Items, deploymentName)) == 1) {
				return false, nil
			}
			return true, nil
		},
		"WaitForAvailablePods", test.ServingNamespace); err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", deploymentName, err)
	}

	ctx.logf("The Revision should remain ready after scaling to one.")
	if err := v1test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, v1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to one: %v", ctx.names.Revision, err)
	}

	ctx.logf("Scaled down.")
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
		test.ServingNamespace,
		scaleToMinimumTimeout,
	)
}
