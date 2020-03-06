// +build preupgrade

/*
Copyright 2018 The Knative Authors

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

	"knative.dev/serving/pkg/apis/autoscaling"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func TestServicePreUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: serviceName,
		Image:   test.PizzaPlanet1,
	}

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1", //make sure we don't scale to zero during the test
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	url := resources.Service.Status.URL.URL()
	assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)
}

func TestServicePreUpgradeAndScaleToZero(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: scaleToZeroServiceName,
		Image:   test.PizzaPlanet1,
	}

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.WindowAnnotationKey: autoscaling.WindowMin.String(), //make sure we scale to zero quickly
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	url := resources.Service.Status.URL.URL()
	assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resources.Revision), clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}

// TestBYORevisionUpgrade creates a Service that uses the BYO Revision name functionality. This test
// is meant to catch new defaults that break bring your own revision name immutability.
func TestBYORevisionPreUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: byoServiceName,
		Image:   test.PizzaPlanet1,
	}

	if _, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithBYORevisionName(byoRevName)); err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
}
