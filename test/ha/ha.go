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

package ha

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	haReplicas = 2
)

func getLeader(t *testing.T, clients *test.Clients, lease string) (string, error) {
	var leader string
	err := wait.PollImmediate(test.PollInterval, time.Minute, func() (bool, error) {
		lease, err := clients.KubeClient.Kube.CoordinationV1().Leases(system.Namespace()).Get(lease, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("error getting lease %s: %w", lease, err)
		}
		leader = strings.Split(*lease.Spec.HolderIdentity, "_")[0]
		// the leader must be an existing pod
		return podExists(clients, leader)
	})
	return leader, err
}

func waitForPodDeleted(t *testing.T, clients *test.Clients, podName string) error {
	return wait.PollImmediate(test.PollInterval, time.Minute, func() (bool, error) {
		exists, err := podExists(clients, podName)
		return !exists, err
	})
}

func getPublicEndpoints(t *testing.T, clients *test.Clients, revision string) ([]string, error) {
	endpoints, err := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s",
			serving.RevisionLabelKey, revision,
			networking.ServiceTypeKey, networking.ServiceTypePublic,
		),
	})
	if err != nil || len(endpoints.Items) != 1 {
		return nil, fmt.Errorf("no endpoints or error: %w", err)
	}

	addresses := endpoints.Items[0].Subsets[0].Addresses
	hosts := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		hosts = append(hosts, addr.IP)
	}
	return hosts, nil
}

func waitForChangedPublicEndpoints(t *testing.T, clients *test.Clients, revision string, origEndpoints []string) error {
	return wait.PollImmediate(100*time.Millisecond, time.Minute, func() (bool, error) {
		newEndpoints, err := getPublicEndpoints(t, clients, revision)
		return !cmp.Equal(origEndpoints, newEndpoints), err
	})
}

func podExists(clients *test.Clients, podName string) (bool, error) {
	if _, err := clients.KubeClient.Kube.CoreV1().Pods(system.Namespace()).Get(podName, metav1.GetOptions{}); err != nil {
		if apierrs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func waitForDeploymentScale(clients *test.Clients, name string, scale int) error {
	return pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		name,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == int32(scale), nil
		},
		"DeploymentIsScaled",
		system.Namespace(),
		time.Minute,
	)
}

func createPizzaPlanetService(t *testing.T, fopt ...rtesting.ServiceOption) (test.ResourceNames, *v1test.ResourceObjects) {
	t.Helper()
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}
	resources, err := v1test.CreateServiceReady(t, clients, &names, fopt...)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	assertServiceEventuallyWorks(t, clients, names, resources.Service.Status.URL.URL(), test.PizzaPlanetText1)
	return names, resources
}

func assertServiceEventuallyWorks(t *testing.T, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedText string) {
	t.Helper()
	// Wait for the Service to be ready.
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatal("Service not ready: ", err)
	}
	// Wait for the Service to serve the expected text.
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err)
	}
}
