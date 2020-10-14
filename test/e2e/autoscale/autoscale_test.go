// +build e2e

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

package autoscale

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	// Concurrency must be high enough to avoid the problems with sampling
	// but not high enough to generate scheduling problems.
	containerConcurrency = 6
	targetUtilization    = 0.7
	successRateSLO       = 0.999
	autoscaleSleep       = 500

	autoscaleTestImageName = "autoscale"
)

func TestAutoscaleUpDownUp(t *testing.T) {
	t.Parallel()

	ctx := e2e.SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization)

	e2e.AssertAutoscaleUpToNumPods(ctx, 1, 2, time.After(60*time.Second), true /* quick */)
	e2e.AssertScaleDown(ctx)
	e2e.AssertAutoscaleUpToNumPods(ctx, 0, 2, time.After(60*time.Second), true /* quick */)
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
		target = 10
	}

	ctx := e2e.SetupSvc(t, class, metric, target, targetUtilization)

	t.Log("The autoscaler spins up additional replicas when traffic increases.")
	// note: without the warm-up / gradual increase of load the test is retrieving a 503 (overload) from the envoy

	// Increase workload for 2 replicas for 60s
	// Assert the number of expected replicas is between n-1 and n+1, where n is the # of desired replicas for 60s.
	// Assert the number of expected replicas is n and n+1 at the end of 60s, where n is the # of desired replicas.
	e2e.AssertAutoscaleUpToNumPods(ctx, 1, 2, time.After(60*time.Second), true /* quick */)
	// Increase workload scale to 3 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	e2e.AssertAutoscaleUpToNumPods(ctx, 2, 3, time.After(60*time.Second), true /* quick */)
	// Increase workload scale to 4 replicas, assert between [n-1, n+1] during scale up, assert between [n, n+1] after scaleup.
	e2e.AssertAutoscaleUpToNumPods(ctx, 3, 4, time.After(60*time.Second), true /* quick */)
}

func TestAutoscaleSustaining(t *testing.T) {
	// When traffic increases, a knative app should scale up and sustain the scale
	// as long as the traffic sustains, despite whether it is switching modes between
	// normal and panic.
	t.Parallel()

	ctx := e2e.SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization)
	e2e.AssertAutoscaleUpToNumPods(ctx, 1, 10, time.After(2*time.Minute), false /* quick */)
}

func TestFastScaleToZero(t *testing.T) {
	t.Parallel()

	ctx := e2e.SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	clients := ctx.Clients()

	cfg, err := e2e.AutoscalerCM(clients)
	if err != nil {
		t.Fatal("Error retrieving autoscaler configmap:", err)
	}

	epsL, err := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s",
			serving.RevisionLabelKey, ctx.Resources().Revision.Name,
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
		eps, err := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(context.Background(), epsN, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return resources.ReadyAddressCount(eps) == 0, nil
	}); err != nil {
		t.Fatalf("Did not observe %q to actually be emptied", epsN)
	}

	t.Log("Total time to scale down:", time.Since(st))
}
