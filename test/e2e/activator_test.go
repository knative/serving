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
	"net/http"
	"sync"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logstream"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	rnames "github.com/knative/serving/pkg/reconciler/revision/resources/names"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
)

// TestActivatorOverload makes sure that activator can handle the load when scaling from 0.
// We need to add a similar test for the User pod overload once the second part of overload handling is done.
func TestActivatorOverload(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	const (
		// The number of concurrent requests to hit the activator with.
		concurrency = 100
		// How long the service will process the request in ms.
		serviceSleep = 300
	)

	clients := Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "timeout",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a service with run latest configuration.")
	// Create a service with concurrency 1 that sleeps for N ms.
	// Limit its maxScale to 10 containers, wait for the service to scale down and hit it with concurrent requests.
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{}, func(service *v1alpha1.Service) {
		service.Spec.ConfigurationSpec.Template.Spec.ContainerConcurrency = 1
		service.Spec.ConfigurationSpec.Template.Annotations = map[string]string{"autoscaling.knative.dev/maxScale": "10"}
	})
	if err != nil {
		t.Fatalf("Unable to create resources: %v", err)
	}
	domain := resources.Route.Status.URL.Host

	deploymentName := rnames.Deployment(resources.Revision)
	if err := WaitForScaleToZero(t, deploymentName, clients); err != nil {
		t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, domain, test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating the Spoofing client: %v", err)
	}

	url := fmt.Sprintf("http://%s/?timeout=%d", domain, serviceSleep)

	t.Log("Starting to send out the requests")

	var group sync.WaitGroup
	// Send requests async and wait for the responses.
	for i := 0; i < concurrency; i++ {
		group.Add(1)
		go func() {
			defer group.Done()

			// We need to create a new request per HTTP request because
			// the spoofing client mutates them.
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Errorf("error creating http request: %v", err)
			}

			res, err := client.Do(req)
			if err != nil {
				t.Errorf("unexpected error sending a request, %v", err)
				return
			}

			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want: %d, response: %s", res.StatusCode, http.StatusOK, res)
			}
		}()
	}
	group.Wait()
}
