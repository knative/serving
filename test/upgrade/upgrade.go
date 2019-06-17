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
	"testing"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
)

const (
	// These service names need to be stable, since we use them across
	// multiple "go test" invocations.
	serviceName            = "pizzaplanet-upgrade-service"
	scaleToZeroServiceName = "scale-to-zero-upgrade-service"
)

// Shamelessly cribbed from conformance/service_test.
func assertServiceResourcesUpdated(t *testing.T, clients *test.Clients, names test.ResourceNames, routeDomain, expectedText string) {
	t.Helper()
	// TODO(#1178): Remove "Wait" from all checks below this point.
	_, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		routeDomain,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, routeDomain, expectedText, err)
	}
}
