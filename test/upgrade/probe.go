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

package upgrade

import (
	"context"
	"flag"

	pkgupgrade "knative.dev/pkg/test/upgrade"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

var successFraction = flag.Float64("probe.success_fraction", 1.0, "Fraction of probes required to pass during upgrade.")

// ProbeTest sends requests to Knative services while performing an upgrade
// and verifies the expected success rate.
func ProbeTest() pkgupgrade.BackgroundOperation {
	var clients *test.Clients
	var names *test.ResourceNames
	var prober test.Prober
	return pkgupgrade.NewBackgroundVerification("ProbeTest",
		func(c pkgupgrade.Context) {
			// Setup
			clients = e2e.Setup(c.T)
			names = &test.ResourceNames{
				Service: "upgrade-probe",
				Image:   test.PizzaPlanet1,
			}
			objects, err := v1test.CreateServiceReady(c.T, clients, names)
			if err != nil {
				c.T.Fatal("Failed to create Service:", err)
			}
			url := objects.Service.Status.URL.URL()

			// This polls until we get a 200 with the right body.
			assertServiceResourcesUpdated(c.T, clients, *names, url, test.PizzaPlanetText1)

			prober = test.RunRouteProber(c.T.Logf, clients, url, test.AddRootCAtoTransport(context.Background(), c.T.Logf, clients, test.ServingFlags.HTTPS))
		},
		func(c pkgupgrade.Context) {
			// Verify
			test.EnsureTearDown(c.T, clients, names)
			test.AssertProberSLO(c.T, prober, *successFraction)
		},
	)
}
