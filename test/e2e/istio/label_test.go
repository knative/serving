// +build e2e istio

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

package e2e

import (
	"testing"

	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func TestIstioRevisionLabel_Service(t *testing.T) {
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, _, err := v1test.CreateServiceReady(t, clients, &names)

	if err != nil {
		t.Fatalf("Failed to create Service %s: %v", names.Service, err)
	}

	r := objects.Revision
	revisionLabel := r.Labels["service.istio.io/canonical-revision"]

	if revisionLabel != r.Name {
		t.Errorf("canonical-revision label set incorrected - got: %q want %q",
			revisionLabel,
			r.Name,
		)
	}

	s := objects.Service
	serviceLabel := r.Labels["service.istio.io/canonical-service"]

	if serviceLabel != s.Name {
		t.Errorf("canonical-service label set incorrected - got: %q want %q",
			serviceLabel,
			s.Name,
		)
	}
}

func TestIstioRevisionLabel_Configuration(t *testing.T) {
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		Image:  "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, err := v1test.CreateConfigurationReady(t, clients, &names)

	if err != nil {
		t.Fatalf("Failed to create Configuration %s: %v", names.Service, err)
	}

	r := objects.Revision
	revisionLabel := r.Labels["service.istio.io/canonical-revision"]

	if revisionLabel != r.Name {
		t.Errorf("canonical-revision label set incorrected - got: %q want %q",
			revisionLabel,
			r.Name,
		)
	}

	c := objects.Configuration
	serviceLabel := r.Labels["service.istio.io/canonical-service"]

	if serviceLabel != c.Name {
		t.Errorf("canonical-service label set incorrected - got: %q want %q",
			serviceLabel,
			c.Name,
		)
	}
}
