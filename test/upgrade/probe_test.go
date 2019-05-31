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
	"testing"

	"github.com/knative/serving/test"
	"github.com/knative/serving/test/e2e"
)

func TestProbe(t *testing.T) {
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: "upgrade-probe",
		Image:   image1,
	}
	defer test.TearDown(clients, names)

	objects, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	domain := objects.Service.Status.URL.Host

	// This polls until we get a 200 with the right body.
	assertServiceResourcesUpdated(t, clients, names, domain, "What a spaceport!")

	// Use log.Printf instead of t.Logf because we want to see failures
	// inline with other logs instead of buffered until the end.
	prober := test.RunRouteProber(log.Printf, clients, domain)
	defer test.AssertProberDefault(t, prober)

	// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
	// over, at which point we will finish the test and check the prober.
	_, _ = ioutil.ReadFile("/tmp/prober-signal")
}
