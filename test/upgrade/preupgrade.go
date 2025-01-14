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

package upgrade

import (
	"testing"

	pkgupgrade "knative.dev/pkg/test/upgrade"
	"knative.dev/serving/pkg/apis/autoscaling"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

// ServingPreUpgradeTests is an umbrella function for grouping all Serving pre-upgrade tests.
func ServingPreUpgradeTests() []pkgupgrade.Operation {
	return []pkgupgrade.Operation{
		ServicePreUpgradeTest(),
		ServicePreUpgradeAndScaleToZeroTest(),
		BYORevisionPreUpgradeTest(),
		InitialScalePreUpgradeTest(),
		DeploymentFailurePreUpgrade(),
	}
}

// ServicePreUpgradeTest creates a service before upgrade.
func ServicePreUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("ServicePreUpgradeTest", func(c pkgupgrade.Context) {
		servicePreUpgrade(c.T)
	})
}

func servicePreUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	resources, err := v1test.CreateServiceReady(t, clients, &upgradeServiceNames,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1", // make sure we don't scale to zero during the test
		}),
	)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := resources.Service.Status.URL.URL()
	assertServiceResourcesUpdated(t, clients, upgradeServiceNames, url, test.PizzaPlanetText1)
}

// ServicePreUpgradeAndScaleToZeroTest creates a new service before
// upgrade and wait for it to scale to zero.
func ServicePreUpgradeAndScaleToZeroTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("ServicePreUpgradeAndScaleToZeroTest", func(c pkgupgrade.Context) {
		servicePreUpgradeAndScaleToZero(c.T)
	})
}

func servicePreUpgradeAndScaleToZero(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	resources, err := v1test.CreateServiceReady(t, clients, &scaleToZeroServiceNames,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey: autoscaling.WindowMin.String(), // make sure we scale to zero quickly
		}),
	)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := resources.Service.Status.URL.URL()
	assertServiceResourcesUpdated(t, clients, scaleToZeroServiceNames, url, test.PizzaPlanetText1)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resources.Revision), clients); err != nil {
		t.Fatal("Could not scale to zero:", err)
	}
}

// BYORevisionPreUpgradeTest creates a Service that uses the BYO Revision name functionality. This test
// is meant to catch new defaults that break bring your own revision name immutability.
func BYORevisionPreUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("BYORevisionPreUpgradeTest", func(c pkgupgrade.Context) {
		bYORevisionPreUpgrade(c.T)
	})
}

func bYORevisionPreUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	if _, err := v1test.CreateServiceReady(t, clients, &byoServiceNames,
		rtesting.WithBYORevisionName(byoRevName)); err != nil {
		t.Fatal("Failed to create Service:", err)
	}
}

// InitialScalePreUpgradeTest creates a service and lets it scale down to zero without
// sending any requests to it.
func InitialScalePreUpgradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("InitialScalePreUpgradeTest", func(c pkgupgrade.Context) {
		initialScalePreUpgrade(c.T)
	})
}

func initialScalePreUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	resources, err := v1test.CreateServiceReady(t, clients, &initialScaleServiceNames)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	if err = e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resources.Revision), clients); err != nil {
		t.Fatal("Could not scale to zero:", err)
	}
}
