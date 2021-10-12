// +build e2e

/*
Copyright 2021 The Knative Authors

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

package autotls

import (
	"context"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestHttpRedirect(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}
	t.Parallel()

	ctx := context.Background()

	clients := test.Setup(t, test.ServingFlags.TLSTestNamespace)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service with http enabled annotation")
	resources, err := v1test.CreateServiceReady(t, clients, &names, rtesting.WithServiceAnnotations(map[string]string{networking.HTTPOptionAnnotationKey: "enabled"}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	httpClient := createHTTPClient(t, clients, resources)
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Do not follow redirect.
		return http.ErrUseLastResponse
	}

	// Explicitly set HTTP schema.
	url := resources.Route.Status.URL.URL()
	url.Scheme = "http"

	RuntimeRequest(ctx, t, httpClient, url.String())

	t.Log("Updating the Service with http redirect annotation")
	_, err = v1test.UpdateService(t, clients, names, rtesting.WithServiceAnnotations(map[string]string{networking.HTTPOptionAnnotationKey: "redirected"}))
	if err != nil {
		t.Fatalf("Failed to update Service: %v: %v", names.Service, err)
	}

	// Verify the annotation change is reflected.
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		resp, err := httpClient.Get(url.String())
		if err != nil {
			return true, err
		}
		if resp.StatusCode == http.StatusMovedPermanently {
			return true, nil
		}
		t.Logf("Redirected is not ready yet.")
		return false, nil
	})
	if waitErr != nil {
		t.Fatalf("The Service %s failed to change the HTTP option: %v", names.Service, waitErr)
	}
}
