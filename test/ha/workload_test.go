//go:build e2e
// +build e2e

/*
Copyright 2024 The Knative Authors

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

package ha

import (
	"context"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	revnames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	repetitionCount = 4
)

func TestWorkloadHA(t *testing.T) {
	const minReplicas = 2

	t.Parallel()

	cases := []struct {
		name string
		tbc  string
	}{{
		name: "activator-in-path",
		tbc:  "-1",
	}, {
		name: "activator-not-in-path",
		tbc:  "0",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clients := e2e.Setup(t)
			ctx := context.Background()

			// Create first service that we will continually probe during disruption scenario.
			names, resources := createPizzaPlanetService(t,
				rtesting.WithConfigAnnotations(map[string]string{
					autoscaling.MinScaleAnnotationKey:  strconv.Itoa(minReplicas),
					autoscaling.MaxScaleAnnotationKey:  strconv.Itoa(minReplicas),
					autoscaling.TargetBurstCapacityKey: tc.tbc, // The Activator is only added to the request path during scale from zero scenarios.
				}),
			)

			test.EnsureTearDown(t, clients, &names)
			t.Log("Starting prober")

			deploymentName := revnames.Deployment(resources.Revision)
			if err := pkgTest.WaitForDeploymentScale(ctx, clients.KubeClient, deploymentName, test.ServingFlags.TestNamespace, minReplicas); err != nil {
				t.Fatalf("Deployment %s not scaled to %v: %v", deploymentName, minReplicas, err)
			}

			prober := test.NewProberManager(t.Logf, clients, minProbes, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
			prober.Spawn(resources.Service.Status.URL.URL())
			defer assertSLO(t, prober, 1)

			for range repetitionCount {
				deleteUserPods(t, ctx, clients, names.Service)
			}

			if err := pkgTest.WaitForDeploymentScale(ctx, clients.KubeClient, deploymentName, test.ServingFlags.TestNamespace, minReplicas); err != nil {
				t.Errorf("Deployment %s not scaled to %v: %v", deploymentName, minReplicas, err)
			}
		})
	}
}

func deleteUserPods(t *testing.T, ctx context.Context, clients *test.Clients, serviceName string) {
	// Get user pods
	selector := labels.SelectorFromSet(labels.Set{
		serving.ServiceLabelKey: serviceName,
	})
	pods, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		t.Fatalf("Unable to get pods: %v", err)
	}

	t.Logf("Deleting user pods")
	for i, pod := range pods.Items {
		t.Logf("Deleting pod %q (%v of %v)", pod.Name, i+1, len(pods.Items))
		err := clients.KubeClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Unable to delete pod: %v", err)
		}

		if err := pkgTest.WaitForPodState(ctx, clients.KubeClient, func(p *corev1.Pod) (bool, error) {
			// Always return false. We're oly interested in the error which indicates pod deletion or timeout.
			t.Logf("%q still not deleted - %s", p.Name, time.Now().String())
			return false, nil
		}, pod.Name, pod.Namespace); err != nil {
			if !apierrs.IsNotFound(err) {
				t.Fatalf("expected pod to not be found")
			}
		}

		if err := pkgTest.WaitForPodDeleted(context.Background(), clients.KubeClient, pod.Name, pod.Namespace); err != nil {
			t.Fatalf("Did not observe %s to actually be deleted: %v", pod.Name, err)
		}
		t.Logf("Deleting pod %q finished", pod.Name)
	}
}
