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

package e2e

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/test"
	ping "github.com/knative/serving/test/test_images/grpc-ping/proto"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGRPC(t *testing.T) {
	t.Parallel()
	// Setup
	clients := Setup(t)

	t.Log("Creating route and configuration for grpc-ping")

	options := &test.Options{
		ContainerPorts: []corev1.ContainerPort{
			{Name: "h2c", ContainerPort: 8080},
		},
	}
	names, err := CreateRouteAndConfig(t, clients, "grpc-ping", options)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Waiting for route to be ready")

	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Route %s: %v", names.Route, err)
	}
	domain := route.Status.Domain

	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	names.Revision = config.Status.LatestCreatedRevisionName
	deploymentName := names.Revision + "-deployment"

	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		pkgTest.Retrying(pkgTest.MatchesAny, http.StatusNotFound),
		"gRPCPingReadyToServe",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't return success: %v", names.Route, domain, err)
	}

	host := &domain
	if !test.ServingFlags.ResolvableDomain {
		host, err = spoof.GetServiceEndpoint(clients.KubeClient.Kube)
		if err != nil {
			t.Fatalf("Could not get service endpoint: %v", err)
		}
	}

	t.Logf("Connecting to grpc-ping using host %q and authority %q", *host, domain)

	waitForScaleToZero := func() {
		t.Logf("Waiting for scale to zero")
		err = pkgTest.WaitForDeploymentState(
			clients.KubeClient,
			deploymentName,
			func(d *v1beta1.Deployment) (bool, error) {
				t.Logf("Deployment %q has %d replicas", deploymentName, d.Status.ReadyReplicas)
				return d.Status.ReadyReplicas == 0, nil
			},
			"DeploymentIsScaledDown",
			test.ServingNamespace,
			3*time.Minute,
		)
		if err != nil {
			t.Fatalf("Could not scale to zero: %v", err)
		}
	}

	payload := func(size int) string {
		b := make([]byte, size)
		_, err = rand.Read(b)
		if err != nil {
			t.Fatalf("Error generating payload: %v", err)
		}
		return string(b)
	}

	conn, err := grpc.Dial(
		*host+":80",
		grpc.WithAuthority(domain+":80"),
		grpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	pc := ping.NewPingServiceClient(conn)

	unaryTest := func(t *testing.T) {
		t.Log("Testing unary Ping")

		want := &ping.Request{Msg: "Hello!"}

		got, err := pc.Ping(context.TODO(), want)
		if err != nil {
			t.Fatalf("Couldn't send request: %v", err)
		}

		if got.Msg != want.Msg {
			t.Errorf("Unexpected response. Want %q, got %q", want.Msg, got.Msg)
		}
	}

	streamTest := func(t *testing.T) {
		t.Log("Testing streaming Ping")

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		stream, err := pc.PingStream(ctx)
		if err != nil {
			t.Fatalf("Error creating stream: %v", err)
		}

		count := 3
		for i := 0; i < count; i++ {
			t.Logf("Sending stream %d of %d", i+1, count)

			want := payload(10)

			err = stream.Send(&ping.Request{Msg: want})
			if err != nil {
				t.Fatalf("Error sending request: %v", err)
			}

			resp, err := stream.Recv()
			if err != nil {
				t.Fatalf("Error receiving response: %v", err)
			}

			got := resp.Msg

			if want != got {
				t.Errorf("Unexpected response. Want %q, got %q", want, got)
			}
		}

		stream.CloseSend()

		_, err = stream.Recv()
		if err != io.EOF {
			t.Errorf("Expected EOF, got %v", err)
		}
	}

	t.Run("unary ping", unaryTest)
	t.Run("streaming ping", streamTest)

	waitForScaleToZero()
	t.Run("unary ping after scale-to-zero", unaryTest)

	// TODO(#3239): Fix gRPC streaming after cold start
	// waitForScaleToZero()
	// t.Run("streaming ping after scale-to-zero", streamTest)
}
