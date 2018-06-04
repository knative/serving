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
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	hello "github.com/knative/serving/test/e2e/test_images/helloworld-grpc/proto"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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

	var imagePath string
	imagePath = strings.Join([]string{test.Flags.DockerRepo, "helloworld"}, "/")

	glog.Infof("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath, v1alpha1.RevisionProtocolHTTP)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names) })
	defer TearDown(clients, names)

	glog.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	err = test.WaitForRouteState(clients.Routes, names.Route, func(r *v1alpha1.Route) (bool, error) {
		if cond := r.Status.GetCondition(v1alpha1.RouteConditionReady); cond == nil {
			return false, nil
		} else {
			return cond.Status == corev1.ConditionTrue, nil
		}
	})
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain
	err = test.WaitForEndpointState(clients.Kube, test.Flags.ResolvableDomain, domain, NamespaceName, names.Route, isHelloWorldExpectedOutput())
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
	}
}

func TestHelloWorldGRPC(t *testing.T) {
	clients := Setup(t)

	var imagePath string
	imagePath = strings.Join([]string{test.Flags.DockerRepo, "helloworld-grpc"}, "/")

	log.Println("Creating a new Route and Configuration")
	names, err := CreateRouteAndConfig(clients, imagePath, v1alpha1.RevisionProtocolGRPC)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	test.CleanupOnInterrupt(func() { TearDown(clients, names) })
	defer TearDown(clients, names)

	log.Println("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	err = test.WaitForRouteState(clients.Routes, names.Route, func(r *v1alpha1.Route) (bool, error) {
		return r.Status.IsReady(), nil
	})
	if err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	endpoint, spoofDomain, err := test.FetchEndpointDomain(clients.Kube, test.Flags.ResolvableDomain, domain, NamespaceName, names.Route)
	if err != nil {
		t.Fatalf("Error fetching endpoint domains: %v", err)
	}

	ingressAddress := fmt.Sprintf("%s:%d", strings.TrimPrefix(endpoint, "http://"), 80)
	err = waitForHelloWorldGRPCEndpoint(ingressAddress, spoofDomain)
	if err != nil {
		t.Fatal(err)
	}
}

func waitForHelloWorldGRPCEndpoint(address string, spoofDomain string) error {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
	}
	if spoofDomain != "" {
		opts = append(opts, grpc.WithAuthority(spoofDomain))
	}

	err := wait.PollImmediate(test.RequestInterval, test.RequestTimeout, func() (bool, error) {
		conn, _ := grpc.Dial(address, opts...)
		defer conn.Close()

		client := hello.NewHelloServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		resp, err := client.Hello(ctx, &hello.Request{Msg: "world"})
		if err != nil {
			return true, err
		}

		expectedResponse := "Hello world"
		receivedResponse := resp.GetMsg()

		if receivedResponse == expectedResponse {
			return true, nil
		}
		return false, fmt.Errorf("Did not get expected response message %s, got %s", expectedResponse, receivedResponse)
	})
	return err
}
