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

package multicontainer

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	pkgTest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func TestMultiContainer(t *testing.T) {
	t.Parallel()

	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Images: []string{
			test.ServingContainer,
			test.SidecarContainer,
		},
	}

	containers := []corev1.Container{{
		Image: pkgTest.ImagePath(names.Images[0]),
		Ports: []corev1.ContainerPort{{
			ContainerPort: 8881,
		}},
	}, {
		Image: pkgTest.ImagePath(names.Images[1]),
	}}

	test.EnsureTearDown(t, clients, &names)
	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers = containers
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.MultiContainerResponse))),
		"MulticontainerServesExpectedText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.MultiContainerResponse, err)
	}
}
