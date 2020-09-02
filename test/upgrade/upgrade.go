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

package upgrade

import (
	"fmt"
	"net/url"
	"os"
	"syscall"
	"testing"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	pkgTest "knative.dev/pkg/test"
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

	pipe = "/tmp/prober-signal"
)

// Shamelessly cribbed from conformance/service_test.
func assertServiceResourcesUpdated(t pkgTest.TLegacy, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedText string) {
	t.Helper()
	// TODO(#1178): Remove "Wait" from all checks below this point.
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatal(fmt.Sprintf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err))
	}
}

func createNewService(serviceName string, t *testing.T) {
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

// createPipe create a named pipe. It fails the test if any error except
// already exist happens.
func createPipe(name string, t *testing.T) {
	if err := syscall.Mkfifo(name, 0666); err != nil {
		if !err.Is(os.ErrExist) {
			t.Fatal("Failed to create pipe:", err)
		}
	}

	test.EnsureCleanup(t, func() {
		os.Remove(name)
	})
}
