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
	"io/ioutil"
	"log"
	"os"
	"syscall"
	"testing"

	"github.com/knative/serving/test"
	"github.com/knative/serving/test/e2e"
	v1a1test "github.com/knative/serving/test/v1alpha1"
)

const pipe = "/tmp/prober-signal"

func TestProbe(t *testing.T) {
	// We run the prober as a golang test because it fits in nicely with
	// the rest of our integration tests, and AssertProberDefault needs
	// a *testing.T. Unfortunately, "go test" intercepts signals, so we
	// can't coordinate with the test by just sending e.g. SIGCONT, so we
	// create a named pipe and wait for the upgrade script to write to it
	// to signal that we should stop probing.
	if err := syscall.Mkfifo(pipe, 0666); err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	defer os.Remove(pipe)

	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: "upgrade-probe",
		Image:   test.PizzaPlanet1,
	}
	defer test.TearDown(clients, names)

	objects, err := v1a1test.CreateRunLatestServiceLegacyReady(t, clients, &names, &v1a1test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := objects.Service.Status.URL.Host

	// This polls until we get a 200 with the right body.
	assertServiceResourcesUpdated(t, clients, names, domain, test.PizzaPlanetText1)

	// Use log.Printf instead of t.Logf because we want to see failures
	// inline with other logs instead of buffered until the end.
	prober := test.RunRouteProber(log.Printf, clients, domain)
	defer test.AssertProberDefault(t, prober)

	// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
	// over, at which point we will finish the test and check the prober.
	_, _ = ioutil.ReadFile(pipe)
}
