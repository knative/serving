// +build e2e

/*
Copyright 2019 The Knative Authors

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
	"net/http"
	"net/url"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	rtesting "knative.dev/serving/pkg/testing/v1"
)

func TestCustomResourcesLimits(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	t.Log("Creating a new Route and Configuration")
	withResources := rtesting.WithResourceRequirements(corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("350Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("350Mi"),
		},
	})

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Autoscale,
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, err := v1test.CreateServiceReady(t, clients, &names, withResources)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}
	endpoint := objects.Route.Status.URL.URL()

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		endpoint,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK)),
		"ResourceTestServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error probing %s: %v", endpoint, err)
	}

	sendPostRequest := func(resolvableDomain bool, url *url.URL) (*spoof.Response, error) {
		client, err := pkgTest.NewSpoofingClient(clients.KubeClient, klog.V(4).Infof, url.Hostname(), resolvableDomain)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequest(http.MethodPost, url.String(), nil)
		if err != nil {
			return nil, err
		}
		return client.Do(req)
	}

	bloatAndCheck := func(mb int, wantSuccess bool) {
		expect := "failure"
		if wantSuccess {
			expect = "success"
		}
		klog.V(2).Infof("Bloating by %d MB and expecting %s.\n", mb, expect)
		u, _ := url.Parse(endpoint.String())
		q := u.Query()
		q.Set("bloat", fmt.Sprintf("%d", mb))
		u.RawQuery = q.Encode()
		response, err := sendPostRequest(test.ServingFlags.ResolvableDomain, u)
		if err != nil {
			klog.V(5).Infof("Received error '%+v' from sendPostRequest (may be expected)\n", err)
			if wantSuccess {
				t.Fatalf("Didn't get a response from bloating RAM with %d MBs", mb)
			}
		} else if response.StatusCode == http.StatusOK {
			if !wantSuccess {
				t.Fatalf("We shouldn't have got a response from bloating RAM with %d MBs", mb)
			}
		} else if response.StatusCode == http.StatusBadRequest {
			t.Error("Test Issue: Received BadRequest from test app, which probably means the test & test image are not cooperating with each other.")
		} else {
			// Accept all other StatusCode as failure; different systems could return 404, 502, etc on failure
			klog.V(5).Infof("Received http code '%d' from sendPostRequest; interpreting as failure of bloat\n", response.StatusCode)
			if wantSuccess {
				t.Fatalf("Didn't get a good response from bloating RAM with %d MBs", mb)
			}
		}
	}

	klog.V(0).Infoln("Querying the application to see if the memory limits are enforced.")
	bloatAndCheck(100, true)
	bloatAndCheck(200, true)
	bloatAndCheck(500, false)
}
