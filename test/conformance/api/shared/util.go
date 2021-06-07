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

package shared

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/pool"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
)

const scaleToZeroGracePeriod = 30 * time.Second

// DigestResolutionExceptions holds the set of "registry" domains for which
// digest resolution is not required.  These "registry" domains are generally
// associated with images that aren't actually published to a registry, but
// side-loaded into the cluster's container daemon via an operation like
// `docker load` or `kind load`.
var DigestResolutionExceptions = sets.NewString("kind.local", "ko.local", "dev.local")

// WaitForScaleToZero will wait for the specified deployment to scale to 0 replicas.
// Will wait up to 6 times the scaleToZeroGracePeriod (30 seconds) before failing.
func WaitForScaleToZero(t testing.TB, deploymentName string, clients *test.Clients) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to zero", deploymentName)

	return pkgTest.WaitForDeploymentState(
		context.Background(),
		clients.KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == 0, nil
		},
		"DeploymentIsScaledDown",
		test.ServingNamespace,
		scaleToZeroGracePeriod*6,
	)
}

// ValidateImageDigest validates the image digest.
func ValidateImageDigest(t *testing.T, imageName string, imageDigest string) (bool, error) {
	ref, err := name.ParseReference(pkgTest.ImagePath(imageName))
	if err != nil {
		return false, err
	}
	if DigestResolutionExceptions.Has(ref.Context().RegistryStr()) {
		t.Run("digest validation", func(t *testing.T) {
			t.Skipf("Skipping digest verification due to use of registry domain %s (one of %v)",
				ref.Context().RegistryStr(), DigestResolutionExceptions)
		})
		return true, nil
	}

	if imageDigest == "" {
		return false, errors.New("imageDigest not present")
	}
	digest, err := name.NewDigest(imageDigest)
	if err != nil {
		return false, err
	}

	return ref.Context().String() == digest.Context().String(), nil
}

// sendRequests sends "num" requests to "url", returning a string for each spoof.Response.Body.
func sendRequests(ctx context.Context, client *spoof.SpoofingClient, url *url.URL, num int) ([]string, error) {
	responses := make([]string, num)

	// Launch "num" requests, recording the responses we get in "responses".
	g, gCtx := pool.NewWithContext(ctx, 8, num)
	for i := 0; i < num; i++ {
		// We don't index into "responses" inside the goroutine to avoid a race, see #1545.
		result := &responses[i]
		g.Go(func() error {
			req, err := http.NewRequestWithContext(gCtx, http.MethodGet, url.String(), nil)
			if err != nil {
				return err
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}

			*result = string(resp.Body)
			return nil
		})
	}
	return responses, g.Wait()
}

func substrInList(key string, targets []string) string {
	for _, t := range targets {
		if strings.Contains(key, t) {
			return t
		}
	}
	return ""
}

// checkResponses verifies that each "expectedResponse" is present in "actualResponses" at least "min" times.
func checkResponses(t testing.TB, num, min int, domain string, expectedResponses, actualResponses []string) error {
	// counts maps the expected response body to the number of matching requests we saw.
	counts := make(map[string]int, len(expectedResponses))
	// badCounts maps the unexpected response body to the number of matching requests we saw.
	badCounts := make(map[string]int)

	// counts := eval(
	//   SELECT body, count(*) AS total
	//   FROM $actualResponses
	//   WHERE body IN $expectedResponses
	//   GROUP BY body
	// )
	for i, ar := range actualResponses {
		if er := substrInList(ar, expectedResponses); er != "" {
			counts[er]++
		} else {
			badCounts[ar]++
			t.Logf("For domain %s: got unexpected response for request %d", domain, i)
		}
	}

	// Print unexpected responses for debugging purposes
	for badResponse, count := range badCounts {
		t.Logf("For domain %s: saw unexpected response %q %d times.", domain, badResponse, count)
	}

	// Verify that we saw each entry in "expectedResponses" at least "min" times.
	// check(SELECT body FROM $counts WHERE total < $min)
	totalMatches := 0
	var errMsg []string
	for _, er := range expectedResponses {
		count := counts[er]
		if count < min {
			errMsg = append(errMsg,
				fmt.Sprintf("domain %s failed: want at least %d, got %d for response %q",
					domain, min, count, er))
		}

		t.Logf("For domain %s: wanted at least %d, got %d requests.", domain, min, count)
		totalMatches += count
	}
	// Verify that the total expected responses match the number of requests made.
	if totalMatches < num {
		errMsg = append(errMsg,
			fmt.Sprintf("domain %s: saw expected responses %d times, wanted %d", domain, totalMatches, num))
	}
	if len(errMsg) == 0 {
		// If we made it here, the implementation conforms. Congratulations!
		return nil
	}
	return errors.New(strings.Join(errMsg, ","))
}

// CheckDistribution sends "num" requests to "domain", then validates that
// we see each body in "expectedResponses" at least "min" times.
func CheckDistribution(ctx context.Context, t testing.TB, clients *test.Clients, url *url.URL, num, min int, expectedResponses []string, resolvable bool) error {
	client, err := pkgTest.NewSpoofingClient(ctx, clients.KubeClient, t.Logf, url.Hostname(), resolvable, test.AddRootCAtoTransport(ctx, t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		return err
	}

	t.Logf("Performing %d concurrent requests to %s", num, url)
	actualResponses, err := sendRequests(ctx, client, url, num)
	if err != nil {
		return err
	}

	return checkResponses(t, num, min, url.Hostname(), expectedResponses, actualResponses)
}
