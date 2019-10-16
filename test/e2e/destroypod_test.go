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
	"net/url"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/davecgh/go-spew/spew"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1a1opts "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	timeoutExpectedOutput   = "Slept for 0 milliseconds"
	revisionTimeoutSeconds  = 45
	timeoutRequestDuration  = 35 * time.Second
	activatorRespawnTimeout = 5 * time.Minute
)

// testToDestroy for table-driven testing.
var testToDestroy = []struct {
	name   string
	rmFunc func(*test.Clients) error
}{
	{"pod", killRevisionPods},
	{"activator", killActivatorPods},
}

func killRevisionPods(clients *test.Clients) error {
	return clients.KubeClient.Kube.CoreV1().Pods(test.ServingNamespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "knative.dev=delete-to-test",
	})
}

func killActivatorPods(clients *test.Clients) error {
	return clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=activator",
	})
}

func TestDestroyPodInflight(t *testing.T) {
	// Not running in parallel as this test deletes activator pods
	clients := Setup(t)

	for _, tc := range testToDestroy {
		t.Run(tc.name, func(t *testing.T) {
			cancel := logstream.Start(t)
			defer cancel()
			testDestroyPodInflight(t, clients, tc.rmFunc)
		})
	}
}

func testDestroyPodInflight(t *testing.T, clients *test.Clients, rmFunc func(*test.Clients) error) {
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "timeout",
	}
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		v1a1opts.WithConfigAnnotations(map[string]string{autoscaling.TargetBurstCapacityKey: "-1"}),
		v1a1opts.WithRevisionTimeoutSeconds(int64(revisionTimeout.Seconds())),
		v1a1opts.WithConfigLabels(map[string]string{"knative.dev": "delete-to-test"}))
	if err != nil {
		t.Fatalf("Failed to create a service: %v", err)
	}
	routeURL := objects.Route.Status.URL.URL()

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		routeURL,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(timeoutExpectedOutput))),
		"TimeoutAppServesText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, routeURL, timeoutExpectedOutput, err)
	}

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, routeURL.Hostname(), test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	// The timeout app sleeps for the time passed via the timeout query parameter in milliseconds
	timeoutRequestDurationInMillis := int64(timeoutRequestDuration / time.Millisecond)
	u, _ := url.Parse(routeURL.String())
	q := u.Query()
	q.Set("timeout", fmt.Sprintf("%d", timeoutRequestDurationInMillis))
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
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
		t.Log("Deleting")
		return rmFunc(clients)
	})

	if err := g.Wait(); err != nil {
		t.Errorf("Something went wrong with the request: %v", err)
	}

	// Make sure activator pods are running for following tests.
	var latestPodState *v1.Pod
	if err := wait.PollImmediate(1*time.Second, activatorRespawnTimeout, func() (bool, error) {
		pods, err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).List(metav1.ListOptions{
			LabelSelector: "app=activator",
		})
		if err != nil {
			return false, nil
		}
		for _, pod := range pods.Items {
			latestPodState = &pod
			for _, status := range pod.Status.ContainerStatuses {
				// There are still containers running, keep retrying.
				if !status.Ready {
					return false, nil
				}
			}
		}
		return true, nil
	}); err != nil {
		t.Logf("Latest state: %s", spew.Sprint(latestPodState))
		t.Fatal("Did not observe activator pods respawn")
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

	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, v1a1opts.WithRevisionTimeoutSeconds(int64(revisionTimeout.Seconds())))
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

func TestDestroyPodWithRequests(t *testing.T) {
	// Not running in parallel as this test deletes activator pods
	clients := Setup(t)

	for _, tc := range testToDestroy {
		t.Run(tc.name, func(t *testing.T) {
			cancel := logstream.Start(t)
			defer cancel()
			testDestroyPodWithRequests(t, clients, tc.rmFunc)
		})
	}
}

func testDestroyPodWithRequests(t *testing.T, clients *test.Clients, rmFunc func(*test.Clients) error) {
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "autoscale",
	}
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		v1a1opts.WithConfigAnnotations(map[string]string{autoscaling.TargetBurstCapacityKey: "-1"}),
		v1a1opts.WithRevisionTimeoutSeconds(int64(revisionTimeout.Seconds())),
		v1a1opts.WithConfigLabels(map[string]string{"knative.dev": "delete-to-test"}))
	if err != nil {
		t.Fatalf("Failed to create a service: %v", err)
	}
	routeURL := objects.Route.Status.URL.URL()

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		routeURL,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"RouteServes",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't serve correctly: %v", names.Route, routeURL, err)
	}

	// The request will sleep for more than 25 seconds.
	u, _ := url.Parse(routeURL.String())
	q := u.Query()
	q.Set("sleep", "25001")
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatalf("Error creating HTTP request: %v", err)
	}
	httpClient, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, u.Hostname(), test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	// Start several requests staggered with 1s delay.
	var eg errgroup.Group
	for i := 1; i < 7; i++ {
		i := i
		t.Logf("Starting request %d at %v", i, time.Now())
		eg.Go(func() error {
			res, err := httpClient.Do(req)
			t.Logf("Request %d done at %v", i, time.Now())
			if err != nil {
				return err
			}
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("request status = %v, want StatusOK", res.StatusCode)
			}
			return nil
		})
		time.Sleep(time.Second)
	}

	t.Log("Deleting")
	// And immeditately kill the pod or activators.
	if err := rmFunc(clients); err != nil {
		t.Fatalf("Error deleting pods: %v", err)
	}

	// Make sure all the requests succeed.
	if err := eg.Wait(); err != nil {
		t.Errorf("Not all requests finished with success, eg: %v", err)
	}

	// Make sure activator pods are running for following tests.
	var latestPodState *v1.Pod
	if err := wait.PollImmediate(1*time.Second, activatorRespawnTimeout, func() (bool, error) {
		pods, err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).List(metav1.ListOptions{
			LabelSelector: "app=activator",
		})
		if err != nil {
			return false, nil
		}
		for _, pod := range pods.Items {
			latestPodState = &pod
			for _, status := range pod.Status.ContainerStatuses {
				// There are still containers not running, keep retrying.
				if !status.Ready {
					return false, nil
				}
			}
		}
		return true, nil
	}); err != nil {
		t.Logf("Latest state: %s", spew.Sprint(latestPodState))
		t.Fatal("Did not observe activator pods respawn")
	}
}
