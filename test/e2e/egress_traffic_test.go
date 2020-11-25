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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	targetHostEnvName = "TARGET_HOST"
	targetHostDomain  = "www.google.com"
)

func TestEgressTraffic(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "httpproxy",
	}
	test.EnsureTearDown(t, clients, &names)

	service, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithEnv(corev1.EnvVar{
			Name:  targetHostEnvName,
			Value: targetHostDomain,
		}))

	if err != nil {
		t.Fatal("Failed to create a service:", err)
	}
	if service.Route.Status.URL == nil {
		t.Fatalf("Can't get internal request domain: service.Route.Status.URL is nil")
	}
	t.Log("Service URL: " + service.Route.Status.URL.String())

	url := service.Route.Status.URL.URL()
	if _, err = pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(spoof.IsStatusOK),
		"HTTPProxy",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Error("Failed to send request to httpproxy:", err)
	}
}
