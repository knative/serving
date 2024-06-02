//go:build e2e
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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestMultiContainer(t *testing.T) {
	if !test.ServingFlags.EnableBetaFeatures {
		t.Skip()
	}
	t.Parallel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.ServingContainer,
		Sidecars: []string{
			test.SidecarContainer,
		},
	}

	containers := []corev1.Container{{
		Image: pkgTest.ImagePath(names.Image),
		Ports: []corev1.ContainerPort{{
			ContainerPort: 8881,
		}},
		Env: []corev1.EnvVar{
			{Name: "FORWARD_PORT", Value: "8882"},
		},
	}, {
		Image: pkgTest.ImagePath(names.Sidecars[0]),
	}}

	// Please see the comment in test/v1/configuration.go.
	if !test.ServingFlags.DisableOptionalAPI {
		for _, c := range containers {
			c.ImagePullPolicy = corev1.PullIfNotPresent
		}
	}

	test.EnsureTearDown(t, clients, &names)
	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers = containers
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.MultiContainerResponse)),
		"MulticontainerServesExpectedText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.MultiContainerResponse, err)
	}
}
