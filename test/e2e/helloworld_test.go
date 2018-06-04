// +build e2e

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

package e2e

import (
	"log"
	"strings"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	helloWorldExpectedOutput = "Hello World! How about some tasty noodles?"
)

func isHelloWorldExpectedOutput() func(body string) (bool, error) {
	return func(body string) (bool, error) {
		return strings.TrimRight(body, "\n") == helloWorldExpectedOutput, nil
	}
}

func TestHelloWorld(t *testing.T) {
	clients := Setup(t)
	defer TearDown(clients)
	test.CleanupOnInterrupt(func() { TearDown(clients) })

	var imagePath string
	imagePath = strings.Join([]string{test.Flags.DockerRepo, "helloworld"}, "/")

	log.Println("Creating a new Route and Configuration")
	err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}

	log.Println("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	err = test.WaitForRouteState(clients.Routes, RouteName, func(r *v1alpha1.Route) (bool, error) {
		return r.Status.IsReady(), nil
	})
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", RouteName, err)
	}

	route, err := clients.Routes.Get(RouteName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", RouteName, err)
	}
	domain := route.Status.Domain
	err = test.WaitForEndpointState(clients.Kube, test.Flags.ResolvableDomain, domain, NamespaceName, RouteName, isHelloWorldExpectedOutput())
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", RouteName, domain, helloWorldExpectedOutput, err)
	}
}
