// +build probe

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
	"io/ioutil"
	"testing"

	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const probePipe = "/tmp/prober-signal"

var successFraction = flag.Float64("probe.success_fraction", 1.0, "Fraction of probes required to pass during upgrade.")

func TestProbe(t *testing.T) {
	t.Parallel()
	// We run the prober as a golang test because it fits in nicely with
	// the rest of our integration tests, and AssertProberDefault needs
	// a *testing.T. Unfortunately, "go test" intercepts signals, so we
	// can't coordinate with the test by just sending e.g. SIGCONT, so we
	// create a named pipe and wait for the upgrade script to write to it
	// to signal that we should stop probing.
	createPipe(t, probePipe)

	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: "upgrade-probe",
		Images:  []string{test.PizzaPlanet1},
	}

	test.EnsureTearDown(t, clients, &names)

	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := objects.Service.Status.URL.URL()

	// This polls until we get a 200 with the right body.
	assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)

	prober := test.RunRouteProber(t.Logf, clients, url, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	defer test.CheckSLO(*successFraction, t.Name(), prober)

	// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
	// over, at which point we will finish the test and check the prober.
	ioutil.ReadFile(probePipe)
}
