//go:build e2e
// +build e2e

/*
Copyright 2021 The Knative Authors

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

package initcontainers

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

// TestInitContainers tests init containers support.
func TestInitContainers(t *testing.T) {
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

	withUserContainerEnvVar := WithEnv(corev1.EnvVar{
		Name:  "SKIP_DATA_WRITE",
		Value: "True",
	})

	withInitContainer := WithInitContainer(corev1.Container{
		Name:  "initsetup",
		Image: pkgTest.ImagePath(test.Volumes),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "data",
			MountPath: "/data",
		}},
		Env: []corev1.EnvVar{{
			Name:  "SKIP_DATA_SERVE",
			Value: "True",
		}},
	})

	resources, err := v1test.CreateServiceReady(t, clients, &names, withVolume, withUserContainerEnvVar, withInitContainer)
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
		"InitContainersText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.EmptyDirText, err)
	}

	revisionName, err := e2e.RevisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	if err := v1test.CheckRevisionState(clients.ServingClient, revisionName, func(r *v1.Revision) (bool, error) {
		if len(r.Status.InitContainerStatuses) != 1 {
			return true, errors.New("init image digest resolution failed")
		}
		initContainerStatus := r.Status.InitContainerStatuses[0]
		if validDigest, err := shared.ValidateImageDigest(t, names.Image, initContainerStatus.ImageDigest); !validDigest {
			return false, fmt.Errorf("imageDigest %s is not valid for imageName %s: %w", initContainerStatus.ImageDigest, initContainerStatus.Name, err)
		}
		if initContainerStatus.Name != "initsetup" {
			return false, errors.New("init image digest resolution failed container name does not match")
		}
		return true, nil
	}); err != nil {
		t.Fatal("Failed to validate revision state:", err)
	}
}
