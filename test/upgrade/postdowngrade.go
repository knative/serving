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
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

// ServingPostDowngradeTests is an umbrella function for grouping all Serving post-downgrade tests.
func ServingPostDowngradeTests() []pkgupgrade.Operation {
	return []pkgupgrade.Operation{
		ServicePostDowngradeTest(),
		CreateNewServicePostDowngradeTest(),
	}
}

// ServicePostDowngradeTest verifies an existing service after downgrade.
func ServicePostDowngradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("ServicePostDowngradeTest", func(c pkgupgrade.Context) {
		servicePostDowngrade(c.T)
	})
}

func servicePostDowngrade(t *testing.T) {
	t.Parallel()
	test.EnsureTearDown(t, e2e.Setup(t), &upgradeServiceNames)
	updateServiceAndCheck(t,
		upgradeServiceNames.Service,
		test.PizzaPlanet1,
		test.PizzaPlanetText2,
		test.PizzaPlanetText1,
	)
}

// CreateNewServicePostDowngradeTest verifies creating a new service after downgrade.
func CreateNewServicePostDowngradeTest() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("CreateNewServicePostDowngradeTest", func(c pkgupgrade.Context) {
		createNewService("pizzaplanet-post-downgrade-service", c.T)
	})
}
