//go:build e2e
// +build e2e

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

package pvc

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	unprivilegedUserID = 65532
)

// TestPersistentVolumeClaims tests pvc support.
func TestPersistentVolumeClaims(t *testing.T) {
	if !test.ServingFlags.EnableBetaFeatures {
		t.Skip("Beta features not enabled")
	}
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.Volumes,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	withVolume := WithVolume("data", "/data", corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "knative-pv-claim",
			ReadOnly:  false,
		},
	})

	// make sure default user can access the written file
	withPodSecurityContext := WithPodSecurityContext(corev1.PodSecurityContext{
		FSGroup: ptr.Int64(unprivilegedUserID),
	})

	resources, err := v1test.CreateServiceReady(t, clients, &names, withVolume, withPodSecurityContext)
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
		"PVCText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.EmptyDirText, err)
	}
}
