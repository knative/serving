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
	"net/url"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/system"
	"knative.dev/pkg/test/logstream/v2"
	pkgupgrade "knative.dev/pkg/test/upgrade"

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
	byoServiceName = "byo-revision-name-upgrade-test"
	byoRevName     = byoServiceName + "-" + "rev1"
)

var (
	// These service names need to be stable, since we use them across
	// multiple tests.
	upgradeServiceNames = test.ResourceNames{
		Service: "pizzaplanet-upgrade-service",
		Image:   test.PizzaPlanet1,
	}
	scaleToZeroServiceNames = test.ResourceNames{
		Service: "scale-to-zero-upgrade-service",
		Image:   test.PizzaPlanet1,
	}
	byoServiceNames = test.ResourceNames{
		Service: byoServiceName,
		Image:   test.PizzaPlanet1,
	}
	initialScaleServiceNames = test.ResourceNames{
		Service: "init-scale-service",
		Image:   test.PizzaPlanet1,
	}
)

// Shamelessly cribbed from conformance/service_test.
func assertServiceResourcesUpdated(t testing.TB, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedText string) {
	t.Helper()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(expectedText)),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err)
	}
}

func createNewService(serviceName string, t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: serviceName,
		Image:   test.PizzaPlanet1,
	}
	test.EnsureTearDown(t, clients, &names)

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := resources.Service.Status.URL.URL()
	assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)
}

func CreateTestNamespace() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("CreateTestNamespace", func(c pkgupgrade.Context) {
		createTestNamespace(c.T, test.ServingFlags.TestNamespace)
	})
}

func createTestNamespace(t *testing.T, ns string) {
	clients := e2e.Setup(t)
	if _, err := clients.KubeClient.CoreV1().Namespaces().
		Create(context.Background(),
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
			metav1.CreateOptions{}); err != nil && !apierrs.IsAlreadyExists(err) {
		t.Fatalf("Couldn't create namespace %q: %v", ns, err)
	}
}

func streamLogs(t *testing.T, clients *test.Clients, serviceName string) logstream.Canceler {
	testStream := logstream.New(context.Background(), clients.KubeClient,
		logstream.WithNamespaces(test.ServingFlags.TestNamespace),
		logstream.WithLineFiltering(false),
		logstream.WithPodPrefixes(serviceName))
	testStreamCancel, err := testStream.StartStream(serviceName, t.Logf)
	if err != nil {
		t.Fatalf("Unable to stream logs from test namespace %s: %v", test.ServingFlags.TestNamespace, err)
	}

	sysStream := logstream.New(context.Background(), clients.KubeClient,
		logstream.WithNamespaces(strings.Split(system.Namespace(), ",")...))
	sysStreamCancel, err := sysStream.StartStream(serviceName, t.Logf)
	if err != nil {
		t.Fatal("Unable to stream logs from system namespaces", err)
	}

	return func() {
		testStreamCancel()
		sysStreamCancel()
	}
}
