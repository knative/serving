/*
Copyright 2018 Google Inc. All Rights Reserved.
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

package conformance

import (
	"fmt"
	"log"
	"strings"
	"testing"

	"encoding/json"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"github.com/mattbaird/jsonpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	namespaceName = "pizzaplanet"
	image1        = "pizzaplanetv1"
	image2        = "pizzaplanetv2"
	configName    = "prod"
	routeName     = "pizzaplanet"
	ingressName   = routeName + "-ingress"
)

func createRouteAndConfig(clients *test.Clients, imagePaths []string) error {
	_, err := clients.Configs.Create(test.Configuration(namespaceName, configName, imagePaths[0]))
	if err != nil {
		return err
	}
	_, err = clients.Routes.Create(test.Route(namespaceName, routeName, configName))
	return err
}

func getFirstRevisionName(clients *test.Clients) (string, error) {
	var revisionName string
	err := test.WaitForConfigurationState(clients.Configs, configName, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != "" {
			revisionName = c.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	})
	return revisionName, err
}

func updateConfigWithImage(clients *test.Clients, imagePaths []string) error {
	patches := []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/revisionTemplate/spec/container/image",
			Value:     imagePaths[1],
		},
	}
	patchBytes, err := json.Marshal(patches)
	newConfig, err := clients.Configs.Patch(configName, types.JSONPatchType, patchBytes, "")
	if err != nil {
		return err
	}
	if newConfig.Generation != int64(2) {
		return fmt.Errorf("The spec was updated so the Generation should be 2 but it was actually %d", newConfig.Generation)
	}
	return nil
}

func assertResourcesUpdatedWhenRevisionIsReady(t *testing.T, clients *test.Clients, revisionName string, expectedText string) {
	log.Println("The Revision will be marked as Ready when it can serve traffic")
	err := test.WaitForRevisionState(clients.Revisions, revisionName, test.IsRevisionReady(revisionName))
	if err != nil {
		t.Fatalf("Revision %s did not become ready to serve traffic: %v", revisionName, err)
	}

	log.Println("Updates the Configuration that the Revision is ready")
	err = test.WaitForConfigurationState(clients.Configs, configName, func(c *v1alpha1.Configuration) (bool, error) {
		return c.Status.LatestReadyRevisionName == revisionName, nil
	})
	if err != nil {
		t.Fatalf("The Configuration %s was not updated indicating that the Revision %s was ready: %v", configName, revisionName, err)
	}

	log.Println("Updates the Route to route traffic to the Revision")
	err = test.WaitForRouteState(clients.Routes, routeName, test.AllRouteTrafficAtRevision(routeName, revisionName))
	if err != nil {
		t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", routeName, revisionName, err)
	}

	log.Println("When the Revision can have traffic routed to it, the Route is marked as Ready")
	err = test.WaitForRouteState(clients.Routes, routeName, func(r *v1alpha1.Route) (bool, error) {
		return r.Status.IsReady(), nil
	})
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic to Revision %s: %v", routeName, revisionName, err)
	}

	log.Println("Serves the expected data at the endpoint")
	updatedRoute, err := clients.Routes.Get(routeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", routeName, err)
	}
	err = test.WaitForEndpointState(clients.Kube, test.Flags.ResolvableDomain, updatedRoute.Status.Domain, namespaceName, routeName, func(body string) (bool, error) {
		return body == expectedText, nil
	})
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", routeName, updatedRoute.Status.Domain, expectedText, err)
	}
}

func getNextRevisionName(clients *test.Clients, prevRevisionName string) (string, error) {
	var newRevisionName string
	err := test.WaitForConfigurationState(clients.Configs, configName, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != prevRevisionName {
			newRevisionName = c.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	})
	return newRevisionName, err
}

func setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(test.Flags.Kubeconfig, test.Flags.Cluster, namespaceName)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

func tearDown(clients *test.Clients) {
	if clients != nil {
		clients.Delete([]string{routeName}, []string{configName})
	}
}

func TestRouteCreation(t *testing.T) {
	clients := setup(t)
	defer tearDown(clients)
	test.CleanupOnInterrupt(func() { tearDown(clients) })

	var imagePaths []string
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image1}, "/"))
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image2}, "/"))

	log.Println("Creating a new Route and Configuration")
	err := createRouteAndConfig(clients, imagePaths)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}

	log.Println("The Configuration will be updated with the name of the Revision once it is created")
	revisionName, err := getFirstRevisionName(clients)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", configName, err)
	}

	assertResourcesUpdatedWhenRevisionIsReady(t, clients, revisionName, "What a spaceport!")

	log.Println("Updating the Configuration to use a different image")
	err = updateConfigWithImage(clients, imagePaths)
	if err != nil {
		t.Fatalf("Patch update for Configuration %s with new image %s failed: %v", configName, imagePaths[1], err)
	}

	log.Println("Since the Configuration was updated a new Revision will be created and the Configuration will be updated")
	revisionName, err = getNextRevisionName(clients, revisionName)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the Revision for image %s: %v", configName, image2, err)
	}

	assertResourcesUpdatedWhenRevisionIsReady(t, clients, revisionName, "Re-energize yourself with a slice of pepperoni!")
}
