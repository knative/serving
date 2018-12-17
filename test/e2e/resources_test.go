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

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCustomResourcesLimits(t *testing.T) {
	clients = Setup(t)

	//add test case specific name to its own logger
	logger = logging.GetContextLogger("TestCustomResourcesLimits")

	var imagePath = test.ImagePath("bloatingcow")

	logger.Infof("Creating a new Route and Configuration")
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("350Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("350Mi"),
		},
	}
	names, err := CreateRouteAndConfig(clients, logger, imagePath, &test.Options{ContainerResources: resources})
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
	defer TearDown(clients, names, logger)

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	logger.Info("Waiting for pod list to contain desired resource configuration")
	err = pkgTest.WaitForPodListState(
		clients.KubeClient,
		func(p *corev1.PodList) (bool, error) {
			for _, pod := range p.Items {
				if strings.HasPrefix(pod.Name, names.Config) {
					for _, c := range pod.Spec.Containers {
						if c.Name == "user-container" {
							want := corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("350Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("350Mi"),
									corev1.ResourceCPU:    resource.MustParse("400m"), // Default value
								},
							}

							if !equality.Semantic.DeepEqual(c.Resources, want) {
								return false, fmt.Errorf("invalid resource configuration for pod %v. Want: %+v, got: %+v", pod.Name, want, c.Resources)
							}
							return true, nil
						}
					}
				}
			}
			return false, nil
		},
		"WaitForAvailablePods", test.ServingNamespace)
	if err != nil {
		logger.Fatalf(`Waiting for Pod.List to have pods with custom resources: %v`, err)
	}

	logger.Info("pods are running with the desired configuration.")

	want := "Moo!"

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(pkgTest.MatchesBody(want), http.StatusNotFound),
		"ResourceTestServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}

	pokeCowForMB := func(mb int) error {
		response, err := sendPostRequest(test.ServingFlags.ResolvableDomain, domain, fmt.Sprintf("memory_in_mb=%d", mb))
		if err != nil {
			return err
		}
		if want != strings.TrimSpace(string(response.Body)) {
			return fmt.Errorf("the response %q is not equal to expected response %q", string(response.Body), want)
		}
		return nil
	}

	logger.Info("Querying the application to see if the memory limits are enforced.")
	if err := pokeCowForMB(100); err != nil {
		t.Fatalf("Didn't get a response from bloating cow with %d MBs of Memory: %v", 100, err)
	}

	if err := pokeCowForMB(200); err != nil {
		t.Fatalf("Didn't get a response from bloating cow with %d MBs of Memory: %v", 200, err)
	}

	if err := pokeCowForMB(500); err == nil {
		t.Fatalf("We shouldn't have got a response from bloating cow with %d MBs of Memory: %v", 500, err)
	}
}

func sendPostRequest(resolvableDomain bool, domain string, query string) (*spoof.Response, error) {
	logger.Infof("The domain of request is %s and its query is %s", domain, query)
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, resolvableDomain)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s?%s", domain, query), nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
