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

package conformance

import (
	"fmt"
	"github.com/golang/glog"
	"strings"
	"testing"

	"encoding/json"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	"github.com/mattbaird/jsonpatch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	namespaceName = "pizzaplanet"
	image1        = "pizzaplanetv1"
	image2        = "pizzaplanetv2"
)

func createRouteAndConfig(clients *test.Clients, names test.ResourceNames, imagePaths []string) error {
	_, err := clients.Configs.Create(test.Configuration(namespaceName, names, imagePaths[0]))
	if err != nil {
		return err
	}
	_, err = clients.Routes.Create(test.Route(namespaceName, names))
	return err
}

func updateConfigWithImage(clients *test.Clients, names test.ResourceNames, imagePaths []string) error {
	patches := []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/revisionTemplate/spec/container/image",
			Value:     imagePaths[1],
		},
	}
	patchBytes, err := json.Marshal(patches)
	newConfig, err := clients.Configs.Patch(names.Config, types.JSONPatchType, patchBytes, "")
	if err != nil {
		return err
	}
	if newConfig.Spec.Generation != int64(2) {
		return fmt.Errorf("The spec was updated so the Generation should be 2 but it was actually %d", newConfig.Spec.Generation)
	}
	return nil
}

func assertResourcesUpdatedWhenRevisionIsReady(t *testing.T, clients *test.Clients, names test.ResourceNames, expectedText string) {
	glog.Infof("When the Route reports as Ready, everything should be ready.")
	err := test.WaitForRouteState(clients.Routes, names.Route, func(r *v1alpha1.Route) (bool, error) {
		if cond := r.Status.GetCondition(v1alpha1.RouteConditionReady); cond == nil {
			return false, nil
		} else {
			return cond.Status == corev1.ConditionTrue, nil
		}
	})
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic to Revision %s: %v", names.Route, names.Revision, err)
	}

	// TODO(#1178): Remove "Wait" from all checks below this point.
	glog.Infof("Serves the expected data at the endpoint")
	updatedRoute, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	err = test.WaitForEndpointState(clients.Kube, test.Flags.ResolvableDomain, updatedRoute.Status.Domain, namespaceName, names.Route, func(body string) (bool, error) {
		return body == expectedText, nil
	})
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, updatedRoute.Status.Domain, expectedText, err)
	}

	// We want to verify that the endpoint works as soon as Ready: True, but there are a bunch of other pieces of state that we validate for conformance.
	glog.Infof("The Revision will be marked as Ready when it can serve traffic")
	err = test.CheckRevisionState(clients.Revisions, names.Revision, test.IsRevisionReady(names.Revision))
	if err != nil {
		t.Fatalf("Revision %s did not become ready to serve traffic: %v", names.Revision, err)
	}
	glog.Infof("Updates the Configuration that the Revision is ready")
	err = test.CheckConfigurationState(clients.Configs, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		return c.Status.LatestReadyRevisionName == names.Revision, nil
	})
	if err != nil {
		t.Fatalf("The Configuration %s was not updated indicating that the Revision %s was ready: %v\n", names.Config, names.Revision, err)
	}
	glog.Infof("Updates the Route to route traffic to the Revision")
	err = test.CheckRouteState(clients.Routes, names.Route, test.AllRouteTrafficAtRevision(names.Route, names.Revision))
	if err != nil {
		t.Fatalf("The Route %s was not updated to route traffic to the Revision %s: %v", names.Route, names.Revision, err)
	}
}

func getNextRevisionName(clients *test.Clients, names test.ResourceNames) (string, error) {
	var newRevisionName string
	err := test.WaitForConfigurationState(clients.Configs, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
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

func tearDown(clients *test.Clients, names test.ResourceNames) {
	if clients != nil {
		clients.Delete([]string{names.Route}, []string{names.Config})
	}
}

func TestRouteCreation(t *testing.T) {
	clients := setup(t)

	var imagePaths []string
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image1}, "/"))
	imagePaths = append(imagePaths, strings.Join([]string{test.Flags.DockerRepo, image2}, "/"))

	var names test.ResourceNames
	names.Config = test.AppendRandomString("prod")
	names.Route = test.AppendRandomString("pizzaplanet")

	test.CleanupOnInterrupt(func() { tearDown(clients, names) })
	defer tearDown(clients, names)

	glog.Infof("Creating a new Route and Configuration")
	err := createRouteAndConfig(clients, names, imagePaths)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}

	glog.Infof("The Configuration will be updated with the name of the Revision once it is created")
	revisionName, err := getNextRevisionName(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	names.Revision = revisionName

	assertResourcesUpdatedWhenRevisionIsReady(t, clients, names, "What a spaceport!")

	glog.Infof("Updating the Configuration to use a different image")
	err = updateConfigWithImage(clients, names, imagePaths)
	if err != nil {
		t.Fatalf("Patch update for Configuration %s with new image %s failed: %v", names.Config, imagePaths[1], err)
	}

	glog.Infof("Since the Configuration was updated a new Revision will be created and the Configuration will be updated")
	revisionName, err = getNextRevisionName(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the Revision for image %s: %v", names.Config, image2, err)
	}
	names.Revision = revisionName

	assertResourcesUpdatedWhenRevisionIsReady(t, clients, names, "Re-energize yourself with a slice of pepperoni!")
}
