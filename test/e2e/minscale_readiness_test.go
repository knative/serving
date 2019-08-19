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
	"fmt"
	"strconv"
	"time"

	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgTest "knative.dev/pkg/test"
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

	name := test.ObjectNameForTest(t)

	names := test.ResourceNames{
		Config: name,
		Route:  name,
		Image:  "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating configuration")
	if _, err := v1a1test.CreateConfiguration(t, clients, names, withMinScale(minScale)); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	revName := latestRevisionName(t, clients, names.Config)
	deploymentName := revName + "-deployment"

	// Revision should reach minScale before becoming ready
	t.Log("Waiting for revision to scale to minScale before becoming ready")
	if err := waitForScaleToN(t, clients, deploymentName, minScale); err != nil {
		t.Fatalf("The deployment %q did not scale to %d: %v", deploymentName, minScale, err)
	}

	if err := v1a1test.WaitForRevisionState(clients.ServingAlphaClient, revName, v1a1test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatalf("The Revision %q did not become ready: %v", revName, err)
	}

	// With no route, ignore minScale and scale-to-zero
	t.Log("Waiting for revision to scale to zero after becoming ready")
	if err := waitForScaleToN(t, clients, deploymentName, 0); err != nil {
		t.Fatalf("The deployment %q did not scale to zero: %v", deploymentName, err)
	}

	// Create route
	t.Log("Creating route")
	if _, err := v1a1test.CreateRoute(t, clients, names); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	if err := v1a1test.WaitForRouteState(clients.ServingAlphaClient, names.Route, v1a1test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %q is not ready: %v", names.Route, err)
	}

	// With a route, MinScale should be observed
	t.Log("Waiting for revision to scale to minScale")
	if err := waitForScaleToN(t, clients, deploymentName, minScale); err != nil {
		t.Fatalf("The deployment %q did not scale to %d: %v", deploymentName, minScale, err)
	}
}

func withMinScale(minScale int) func(cfg *v1alpha1.Configuration) {
	return func(cfg *v1alpha1.Configuration) {
		if cfg.Spec.Template.Annotations == nil {
			cfg.Spec.Template.Annotations = make(map[string]string)
		}
		cfg.Spec.Template.Annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(minScale)
	}
}

func latestRevisionName(t *testing.T, clients *test.Clients, configName string) string {
	// Wait for the Config have a LatestCreatedRevisionName
	if err := v1a1test.WaitForConfigurationState(clients.ServingAlphaClient, configName, v1a1test.ConfigurationHasCreatedRevision, "ConfigurationHasCreatedRevision"); err != nil {
		t.Fatalf("The Configuration %q does not have a LatestCreatedRevisionName: %v", configName, err)
	}

	config, err := clients.ServingAlphaClient.Configs.Get(configName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}

	return config.Status.LatestCreatedRevisionName
}

func waitForScaleToN(t *testing.T, clients *test.Clients, deploymentName string, n int) error {
	return pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == 0, nil
		},
		fmt.Sprintf("DeploymentIsScaledTo%d", n),
		test.ServingNamespace,
		3*time.Minute,
	)
}
