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
	"testing"

	corev1 "k8s.io/api/core/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestMultiContainer(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	featureConfigMap, err := rawCM(clients, config.FeaturesConfigName)
	if err != nil {
		t.Errorf("Error retrieving config-feature configmap: %v", err)
	}

	patchedFeatureConfigMap := featureConfigMap.DeepCopy()
	patchedFeatureConfigMap.Data["multi-container"] = "enabled"
	_, err = patchCM(clients, patchedFeatureConfigMap)
	defer patchCM(clients, featureConfigMap)
	test.CleanupOnInterrupt(func() { patchCM(clients, featureConfigMap) })

	containers := []corev1.Container{{
		Image: test.ServingContainer,
		Ports: []corev1.ContainerPort{{
			ContainerPort: 8881,
		}},
	}, {
		Image: test.SidecarContainerOne,
	}, {
		Image: test.SidecarContainerTwo,
	}}

	names := test.ResourceNames{
		Service:    test.ObjectNameForTest(t),
		Containers: containers,
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.MultiContainerResponse))),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.MultiContainerResponse, err)
	}

	revision := resources.Revision
	if val, ok := revision.Labels["serving.knative.dev/configuration"]; ok {
		if val != names.Config {
			t.Fatalf("Expect configuration name in revision label %q but got %q ", names.Config, val)
		}
	} else {
		t.Fatalf("Failed to get configuration name from Revision label")
	}
	if val, ok := revision.Labels["serving.knative.dev/service"]; ok {
		if val != names.Service {
			t.Fatalf("Expect Service name in revision label %q but got %q ", names.Service, val)
		}
	} else {
		t.Fatalf("Failed to get Service name from Revision label")
	}
}
