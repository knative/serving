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
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
)

const scaleToZeroGracePeriod = 30 * time.Second

// WaitForScaleToZero will wait for the specified deployment to scale to 0 replicas.
// Will wait up to 6 times the scaleToZeroGracePeriod (30 seconds) before failing.
func WaitForScaleToZero(t pkgTest.TLegacy, deploymentName string, clients *test.Clients) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to zero", deploymentName)

	return pkgTest.WaitForDeploymentState(
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
func ValidateImageDigest(imageName string, imageDigest string) (bool, error) {
	ref, err := name.ParseReference(pkgTest.ImagePath(imageName))
	if err != nil {
		return false, err
	}

	digest, err := name.NewDigest(imageDigest)
	if err != nil {
		return false, err
	}

	return ref.Context().String() == digest.Context().String(), nil
}

// sendRequests sends "num" requests to "url", returning a string for each spoof.Response.Body.
func sendRequests(client spoof.Interface, url *url.URL, num int) ([]string, error) {
	responses := make([]string, num)

	// Launch "num" requests, recording the responses we get in "responses".
	g, _ := errgroup.WithContext(context.Background())
	for i := 0; i < num; i++ {
		// We don't index into "responses" inside the goroutine to avoid a race, see #1545.
		result := &responses[i]
		g.Go(func() error {
			req, err := http.NewRequest(http.MethodGet, url.String(), nil)
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
func checkResponses(t pkgTest.TLegacy, num, min int, domain string, expectedResponses, actualResponses []string) error {
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
	for _, ar := range actualResponses {
		if er := substrInList(ar, expectedResponses); er != "" {
			counts[er]++
		} else {
			badCounts[ar]++
		}
	}

	// Verify that the total expected responses match the number of requests made.
	for badResponse, count := range badCounts {
		t.Logf("Saw unexpected response %q %d times.", badResponse, count)
	}

	// Verify that we saw each entry in "expectedResponses" at least "min" times.
	// check(SELECT body FROM $counts WHERE total < $min)
	totalMatches := 0
	errMsg := []string{}
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
func CheckDistribution(t pkgTest.TLegacy, clients *test.Clients, url *url.URL, num, min int, expectedResponses []string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, t.Logf, url.Hostname(), test.ServingFlags.ResolvableDomain, test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https))
	if err != nil {
		return err
	}

	t.Logf("Performing %d concurrent requests to %s", num, url)
	actualResponses, err := sendRequests(client, url, num)
	if err != nil {
		return err
	}

	return checkResponses(t, num, min, url.Hostname(), expectedResponses, actualResponses)
}
