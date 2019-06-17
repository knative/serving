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

	revisionresourcenames "github.com/knative/serving/pkg/reconciler/revision/resources/names"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/e2e"
	v1a1test "github.com/knative/serving/test/v1alpha1"
)

func TestRunLatestServicePreUpgrade(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = serviceName
	names.Image = test.PizzaPlanet1

	resources, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names, &v1a1test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := resources.Service.Status.URL.Host
	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)
}

func TestRunLatestServicePreUpgradeAndScaleToZero(t *testing.T) {
	t.Parallel()
	clients := e2e.Setup(t)

	var names test.ResourceNames
	names.Service = scaleToZeroServiceName
	names.Image = test.PizzaPlanet1

	resources, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names, &v1a1test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := resources.Service.Status.URL.Host
	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(resources.Revision), clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}
