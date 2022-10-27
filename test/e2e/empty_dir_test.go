//go:build e2e

/*
Copyright 2022 The Knative Authors

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
	"k8s.io/apimachinery/pkg/api/resource"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestEmptyDir ensure emptyDir volume support works
func TestEmptyDir(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Volumes,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	quantity := resource.MustParse("100M")
	withVolume := WithVolume("data", "/data", corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{
			SizeLimit: &quantity,
		},
	})

	resources, err := v1test.CreateServiceReady(t, clients, &names, withVolume)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.EmptyDirText)),
		"EmptyDir Text",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.EmptyDirText, err)
	}
}
