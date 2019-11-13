// +build e2e

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

package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	pkgTest "knative.dev/pkg/test"
	v1a1opts "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

func TestSingleConcurrency(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.SingleThreadedImage,
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		v1a1opts.WithContainerConcurrency(1))
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}
	url := objects.Service.Status.URL.URL()

	// Ready does not actually mean Ready for a Route just yet.
	// See https://knative.dev/serving/issues/1582
	t.Logf("Probing %s", url)
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Error probing %s: %v", url, err)
	}

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, url.Hostname(), test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("Error creating spoofing client: %v", err)
	}

	concurrency := 5
	duration := 20 * time.Second
	t.Logf("Maintaining %d concurrent requests for %v.", concurrency, duration)
	group, _ := errgroup.WithContext(context.Background())
	for i := 0; i < concurrency; i++ {
		group.Go(func() error {
			done := time.After(duration)
			req, err := http.NewRequest(http.MethodGet, url.String(), nil)
			if err != nil {
				return fmt.Errorf("error creating http request: %w", err)
			}

			for {
				select {
				case <-done:
					return nil
				default:
					res, err := client.Do(req)
					if err != nil {
						return fmt.Errorf("error making request %w", err)
					}
					if res.StatusCode == http.StatusInternalServerError {
						return errors.New("detected concurrent requests")
					} else if res.StatusCode != http.StatusOK {
						return fmt.Errorf("non 200 response %v", res.StatusCode)
					}
				}
			}
		})
	}
	t.Log("Waiting for all requests to complete.")
	if err := group.Wait(); err != nil {
		t.Fatalf("Error making requests for single threaded test: %v.", err)
	}
}
