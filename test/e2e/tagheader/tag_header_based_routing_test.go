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

package tagheader

import (
	"context"
	"net/http"
	"testing"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

// This test creates a Knative Service with two revisions: revision 1 and revision 2.
// Revision 1 has tag "rev1".
// Revision 2 does not have tag. And the Knative Service routes 100% traffic to revision 2.
// In the test, two requests will be sent to the Knative Service:
// 1) request with header "Knative-Serving-Tag: rev1". This request should be routed to revision 1.
// 2) request with header "Knative-Serving-Tag: WrongHeader". This request should be routed to revision 2
// as when header Knative-Serving-Tag does not match any revision tag, the request will fall back to the main Route.
// In order to run this test, the tag hearder based routing feature needs to be turned on:
// https://github.com/knative/serving/blob/master/config/core/configmaps/network.yaml#L115
func TestTagHeaderBasedRouting(t *testing.T) {
	t.Parallel()

	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	// Setup Initial Service
	t.Log("Creating a new Service in runLatest")
	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	test.EnsureTearDown(t, clients, &names)

	revision1 := names.Revision

	// Create a new revision with different image
	t.Log("Updating the Service to use a different image")
	service, err := v1test.PatchService(t, clients, objects.Service, rtesting.WithServiceImage(pkgTest.ImagePath(test.PizzaPlanet2)))
	if err != nil {
		t.Fatalf("Patch update for Service %s with new image %s failed: %v", names.Service, test.PizzaPlanet2, err)
	}
	objects.Service = service

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	revision2, err := v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for image %s: %v", names.Service, test.PizzaPlanet2, err)
	}

	if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:          "rev1",
			RevisionName: revision1,
			Percent:      ptr.Int64(0),
		}, {
			RevisionName: revision2,
			Percent:      ptr.Int64(100),
		}},
	}); err != nil {
		t.Fatal("Failed to update Service:", err)
	}

	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	testCases := []struct {
		name         string
		header       string
		wantResponse string
	}{{
		name:         "send request with correct header",
		header:       "rev1",
		wantResponse: test.PizzaPlanetText1,
	}, {
		name:         "send request with non-exist header",
		header:       "rev-wrong",
		wantResponse: test.PizzaPlanetText2,
	}, {
		name:         "send request without  header",
		wantResponse: test.PizzaPlanetText2,
	}}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := pkgTest.WaitForEndpointState(
				context.Background(),
				clients.KubeClient,
				t.Logf,
				objects.Service.Status.URL.URL(),
				v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(tt.wantResponse))),
				"WaitForSuccessfulResponse",
				test.ServingFlags.ResolvableDomain,
				test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
				addHeader(tt.header),
			); err != nil {
				t.Fatalf("Error probing %s: %v", objects.Service.Status.URL.URL(), err)
			}
		})
	}
}

func addHeader(header string) pkgTest.RequestOption {
	return func(req *http.Request) {
		if len(header) != 0 {
			req.Header.Add(network.TagHeaderName, header)
		}
	}
}
