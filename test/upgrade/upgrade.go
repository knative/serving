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

package upgrade

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	// These service names need to be stable, since we use them across
	// multiple "go test" invocations.
	serviceName              = "pizzaplanet-upgrade-service"
	postUpgradeServiceName   = "pizzaplanet-post-upgrade-service"
	postDowngradeServiceName = "pizzaplanet-post-downgrade-service"
	scaleToZeroServiceName   = "scale-to-zero-upgrade-service"
	byoServiceName           = "byo-revision-name-upgrade-test"
	byoRevName               = byoServiceName + "-" + "rev1"
	initialScaleServiceName  = "init-scale-service"
)

// Shamelessly cribbed from conformance/service_test.
func assertServiceResourcesUpdated(t testing.TB, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedText string) {
	t.Helper()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(spoof.MatchesAllOf(spoof.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
		t.Fatal(fmt.Sprintf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err))
	}
}

func createNewService(serviceName string, t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: serviceName,
		Image:   test.PizzaPlanet1,
	}

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := resources.Service.Status.URL.URL()
	assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)
}
