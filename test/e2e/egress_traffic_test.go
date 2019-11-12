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
	"testing"

	pkgTest "knative.dev/pkg/test"
	v1a1opts "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
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
	envVars := []corev1.EnvVar{{
		Name:  targetHostEnvName,
		Value: targetHostDomain,
	}}
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	service, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		v1a1opts.WithEnv(envVars...))
	if err != nil {
		t.Fatalf("Failed to create a service: %v", err)
	}
	if service.Route.Status.URL == nil {
		t.Fatalf("Can't get internal request domain: service.Route.Status.URL is nil")
	}
	t.Log(service.Route.Status.URL.String())

	url := service.Route.Status.URL.URL()
	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"HTTPProxy",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Failed to send request to httpproxy: %v", err)
	}
}
