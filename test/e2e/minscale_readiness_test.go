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
	"strconv"

	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

func TestMinScale(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	const minScale = 4

	clients := Setup(t)

	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		Route:  test.ObjectNameForTest(t),
		Image:  "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	if _, err := v1a1test.CreateConfiguration(t, clients, names, func(cfg *v1alpha1.Configuration) {
		if cfg.Spec.Template.Annotations == nil {
			cfg.Spec.Template.Annotations = make(map[string]string)
		}

		cfg.Spec.Template.Annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(minScale)

	}); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	// Wait for the Config have a LatestCreatedRevisionName
	if err := v1a1test.WaitForConfigurationState(clients.ServingAlphaClient, names.Config, v1a1test.ConfigurationHasCreatedRevision, "ConfigurationHasCreatedRevision"); err != nil {
		t.Fatalf("The Configuration %q does not have a LatestCreatedRevisionName: %v", names.Config, err)
	}

	// Without a route, MinScale should be ignored
	got := latestAvailableReplicas(t, clients, names)

	if got > 1 {
		t.Fatalf("Reported ready with %d replicas, expected <= 1", got)
	}

	if _, err := v1a1test.CreateRoute(t, clients, names); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	if err := v1a1test.WaitForRouteState(clients.ServingAlphaClient, names.Route, v1a1test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %q is not ready: %v", names.Route, err)
	}

	// With a route, MinScale should be observed
	got = latestAvailableReplicas(t, clients, names)

	if got < int32(minScale) {
		t.Fatalf("Reported ready with %d replicas, expected %d", got, minScale)
	}
}

func latestAvailableReplicas(t *testing.T, clients *test.Clients, names test.ResourceNames) int32 {
	config, err := clients.ServingAlphaClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}

	revName := config.Status.LatestCreatedRevisionName

	if err = v1a1test.WaitForRevisionState(clients.ServingAlphaClient, revName, v1a1test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", revName, err)
	}

	deployment, err := clients.KubeClient.Kube.AppsV1().Deployments(test.ServingNamespace).Get(revName+"-deployment", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Deployment for Revision %s, err: %v", revName, err)
	}

	return deployment.Status.AvailableReplicas
}
