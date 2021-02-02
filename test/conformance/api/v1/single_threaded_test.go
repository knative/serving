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

package v1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
)

func withContainerConcurrency(cc int64) rtesting.ServiceOption {
	return func(svc *v1.Service) {
		svc.Spec.Template.Spec.ContainerConcurrency = &cc
	}
}

func TestSingleConcurrency(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.SingleThreadedImage,
	}
	test.EnsureTearDown(t, clients, &names)

	objects, err := v1test.CreateServiceReady(t, clients, &names, withContainerConcurrency(1))
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := objects.Service.Status.URL.URL()

	// Ready does not actually mean Ready for a Route just yet.
	// See https://github.com/knative/serving/issues/1582
	t.Log("Probing", url)
	if _, err := pkgtest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(spoof.IsStatusOK),
		"WaitForSuccessfulResponse",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
		t.Fatalf("Error probing %s: %v", url, err)
	}

	client, err := pkgtest.NewSpoofingClient(context.Background(), clients.KubeClient, t.Logf, url.Hostname(),
		test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatal("Error creating spoofing client:", err)
	}

	concurrency := 5
	duration := 20 * time.Second
	t.Logf("Maintaining %d concurrent requests for %v.", concurrency, duration)
	group, egCtx := errgroup.WithContext(context.Background())
	for i := 0; i < concurrency; i++ {
		threadIdx := i
		group.Go(func() error {
			requestIdx := 0
			done := time.After(duration)
			req, err := http.NewRequestWithContext(egCtx, http.MethodGet, url.String(), nil)
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
