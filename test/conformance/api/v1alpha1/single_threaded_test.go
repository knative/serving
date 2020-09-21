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
	test.EnsureTearDown(t, clients, &names)

	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		v1a1opts.WithContainerConcurrency(1))
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := objects.Service.Status.URL.URL()

	// Ready does not actually mean Ready for a Route just yet.
	// See https://github.com/knative/serving/issues/1582
	t.Log("Probing", url)
	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", url, err)
	}

	client, err := pkgTest.NewSpoofingClient(context.Background(), clients.KubeClient, t.Logf, url.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatal("Error creating spoofing client:", err)
	}

	concurrency := 5
	duration := 20 * time.Second
	t.Logf("Maintaining %d concurrent requests for %v.", concurrency, duration)
	group, _ := errgroup.WithContext(context.Background())
	for i := 0; i < concurrency; i++ {
		threadIdx := i
		group.Go(func() error {
			requestIdx := 0
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
					requestIdx++
					if err != nil {
						return fmt.Errorf("error making request, thread index: %d, request index: %d: %w", threadIdx, requestIdx, err)
					}
					if res.StatusCode == http.StatusInternalServerError {
						return errors.New("detected concurrent requests")
					} else if res.StatusCode != http.StatusOK {
						return fmt.Errorf("non 200 response, thread index: %d, request index: %d, response %s", threadIdx, requestIdx, res)
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
