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
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	rtesting "knative.dev/serving/pkg/testing/v1"
)

var resourceLimit resource.Quantity

func init() {
	resourceLimit = resource.MustParse(test.ContainerMemoryLimit)
}

func TestMustSetCustomResourcesLimits(legacy *testing.T) {
	t, cancel := logging.NewTLogger(legacy)
	defer cancel()

	clients, names, objects, cancel, err := v1test.CreateBasicServiceTest(t,
		test.Autoscale,
		rtesting.WithResourceRequirements(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resourceLimit,
			},
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resourceLimit,
			},
		}))
	defer cancel()
	t.FatalIfErr(err, "Failed to create initial Service", "name", names.Service)

	// This is in e2e, not conformance, because k8s does not require implementations to terminate
	// See https://github.com/knative/serving/pull/6014#issuecomment-553714724
	endpoint := objects.Route.Status.URL.URL()
	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		endpoint,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK)),
		"ResourceTestServesText",
		test.ServingFlags.ResolvableDomain)
	t.FatalIfErr(err, "Error probing", "URL", endpoint)

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
		t.V(2).Info("Bloating", "MB increase", mb, "want", expect)
		u, _ := url.Parse(endpoint.String())
		q := u.Query()
		q.Set("bloat", fmt.Sprintf("%d", mb))
		u.RawQuery = q.Encode()
		response, err := sendPostRequest(test.ServingFlags.ResolvableDomain, u)
		if err != nil {
			t.V(5).Info("Received error from sendPostRequest (may be expected)", "error", err)
			if wantSuccess {
				t.Error("Didn't get a response from bloating RAM", "MB", mb)
			}
		} else if response.StatusCode == http.StatusOK {
			if !wantSuccess {
				t.Error("We shouldn't have got a response from bloating RAM", "MB", mb)
			}
		} else if response.StatusCode == http.StatusBadRequest {
			t.Error("Test Issue: Received BadRequest from test app, which probably means the test & test image are not cooperating with each other.")
		} else {
			// Accept all other StatusCode as failure; different systems could return 404, 502, etc on failure
			t.V(5).Info("Received non-OK http code from sendPostRequest; interpreting as failure of bloat", "StatusCode", response.StatusCode)
			if wantSuccess {
				t.Error("Didn't get a good response from bloating RAM", "MB", mb)
			}
		}
	}

	t.V(1).Info("Querying the application to see if the memory limits are enforced.")
	bloatAndCheck(100, true)
	bloatAndCheck(200, true)
	bloatAndCheck(500, false)
}
