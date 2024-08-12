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

package runtime

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	v1opts "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	livenessPath        = "/healthz/liveness"
	livenessCounterPath = "/healthz/livenessCounter"
)

func TestLivenessWithFail(t *testing.T) {
	t.Parallel()
	if test.ServingFlags.DisableOptionalAPI {
		t.Skip("Container.livenessProbe is not required by Knative Serving API Specification")
	}
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Readiness,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		v1opts.WithLivenessProbe(
			&corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: livenessPath,
						Port: intstr.FromInt32(8080),
					},
				},
				PeriodSeconds:    1,
				FailureThreshold: 3,
			}))

	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// Wait for the liveness probe to be executed a few times before introducing a failure.
	url := resources.Route.Status.URL.URL()
	url.Path = livenessCounterPath
	if _, err = pkgtest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		atLeastNumLivenessChecks(t, 5),
		"livenessIsReady",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
	}

	// Check that user-container hasn't been restarted yet.
	deploymentName := resourcenames.Deployment(resources.Revision)
	podList, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal("Unable to get pod list: ", err)
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		if strings.Contains(pod.Name, deploymentName) && test.UserContainerRestarted(pod) {
			t.Fatal("User container unexpectedly restarted")
		}
	}

	t.Log("POST to /start-failing")
	client, err := pkgtest.NewSpoofingClient(context.Background(),
		clients.KubeClient,
		t.Logf,
		url.Hostname(),
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatalf("Failed to create spoofing client: %v", err)
	}

	url.Path = "/start-failing"
	startFailing, err := http.NewRequest(http.MethodPost, url.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(startFailing); err != nil {
		t.Fatalf("POST to /start-failing failed: %v", err)
	}

	// Wait for the user-container to be restarted.
	if err := pkgtest.WaitForPodListState(
		context.Background(),
		clients.KubeClient,
		func(p *corev1.PodList) (bool, error) {
			for i := range p.Items {
				pod := &p.Items[i]
				if strings.Contains(pod.Name, deploymentName) && test.UserContainerRestarted(pod) {
					return true, nil
				}
			}
			return false, nil
		},
		"WaitForContainerRestart", test.ServingFlags.TestNamespace); err != nil {
		t.Fatalf("Failed waiting for user-container to be restarted: %v", err)
	}

	// After restart, verify the liveness probe passes a few times again.
	url.Path = livenessCounterPath
	if _, err = pkgtest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		atLeastNumLivenessChecks(t, 5),
		"livenessIsReady",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
	}
}

func atLeastNumLivenessChecks(t *testing.T, expectedChecks int) spoof.ResponseChecker {
	return func(resp *spoof.Response) (bool, error) {
		actualChecks, err := strconv.Atoi(string(resp.Body))
		// Some errors are temporarily expected after container restart.
		if err != nil {
			t.Logf("Unable to parse num checks, status: %d, body: %s", resp.StatusCode, string(resp.Body))
		}
		if actualChecks >= expectedChecks {
			return true, nil
		}
		return false, nil
	}
}
