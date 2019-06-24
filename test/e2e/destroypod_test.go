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
	"net/http"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/davecgh/go-spew/spew"
	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logstream"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	timeoutExpectedOutput  = "Slept for 0 milliseconds"
	revisionTimeoutSeconds = 45
	timeoutRequestDuration = 43 * time.Second
)

func TestDestroyPodInflight(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	t.Log("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(t, clients, "timeout", &v1a1test.Options{RevisionTimeoutSeconds: revisionTimeoutSeconds})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("When the Revision can have traffic routed to it, the Route is marked as Ready")
	if err := v1a1test.WaitForRouteState(clients.ServingAlphaClient, names.Route, v1a1test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingAlphaClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.URL.Host

	err = v1a1test.WaitForConfigurationState(clients.ServingAlphaClient, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			names.Revision = c.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ConfigurationUpdatedWithRevision")
	if err != nil {
		t.Fatalf("Error obtaining Revision's name %v", err)
	}

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(timeoutExpectedOutput))),
		"TimeoutAppServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, test.HelloWorldText, err)
	}

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	// The timeout app sleeps for the time passed via the timeout query parameter in milliseconds
	timeoutRequestDurationInMillis := timeoutRequestDuration / time.Millisecond
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/?timeout=%d", domain, timeoutRequestDurationInMillis), nil)
	if err != nil {
		t.Fatalf("Error creating http request: %v", err)
	}

	g, _ := errgroup.WithContext(context.Background())

	g.Go(func() error {
		t.Log("Sending in a long running request")
		res, err := client.Do(req)
		if err != nil {
			return err
		}

		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("Expected response to have status 200, had %d", res.StatusCode)
		}
		expectedBody := fmt.Sprintf("Slept for %d milliseconds", timeoutRequestDurationInMillis)
		gotBody := string(res.Body)
		if gotBody != expectedBody {
			return fmt.Errorf("Unexpected body, expected: %q got: %q", expectedBody, gotBody)
		}
		return nil
	})

	g.Go(func() error {
		// Give the request a bit of time to be established and reach the pod.
		time.Sleep(timeoutRequestDuration / 2)

		t.Log("Destroying the configuration (also destroys the pods)")
		return clients.ServingAlphaClient.Configs.Delete(names.Config, nil)
	})

	if err := g.Wait(); err != nil {
		t.Errorf("Something went wrong with the request: %v", err)
	}
}

// We choose a relatively high upper boundary for the test to give even a busy
// Kubernetes test system plenty of time to remove the pod quicker than this.
const revisionTimeout = 5 * time.Minute

func TestDestroyPodTimely(t *testing.T) {
	// Not running in parallel on purpose.

	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{RevisionTimeoutSeconds: int64(revisionTimeout.Seconds())})
	if err != nil {
		t.Fatalf("Failed to create a service: %v", err)
	}

	pods, err := clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace).List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", serving.RevisionLabelKey, objects.Revision.Name),
	})
	if err != nil || len(pods.Items) == 0 {
		t.Fatalf("No pods or error: %v", err)
	}

	podToDelete := pods.Items[0].Name
	t.Logf("Deleting pod %q", podToDelete)
	start := time.Now()
	clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace).Delete(podToDelete, &metav1.DeleteOptions{})

	var latestPodState *v1.Pod
	if err := wait.PollImmediate(1*time.Second, revisionTimeout, func() (bool, error) {
		pod, err := clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace).Get(podToDelete, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		latestPodState = pod
		for _, status := range pod.Status.ContainerStatuses {
			// There are still containers running, keep retrying.
			if status.State.Running != nil {
				return false, nil
			}
		}
		return true, nil
	}); err != nil {
		t.Logf("Latest state: %s", spew.Sprint(latestPodState))
		t.Fatalf("Did not observe %q to actually be deleted", podToDelete)
	}

	// Make sure the pod was deleted significantly faster than the revision timeout.
	timeToDelete := time.Since(start)
	if timeToDelete > revisionTimeout-30*time.Second {
		t.Errorf("Time to delete pods = %v, want < %v", timeToDelete, revisionTimeout)
	}
}
