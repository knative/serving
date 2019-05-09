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
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	ingress "github.com/knative/pkg/test/ingress"
	resourcenames "github.com/knative/serving/pkg/reconciler/revision/resources/names"
	"github.com/knative/serving/test"
	ping "github.com/knative/serving/test/test_images/grpc-ping/proto"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
)

type grpcTest func(*testing.T, *test.ResourceObjects, *test.Clients, string, string)

// hasPort checks if a URL contains a port number
func hasPort(u string) bool {
	parts := strings.Split(u, ":")
	_, err := strconv.Atoi(parts[len(parts)-1])
	return err == nil
}

func dial(host, domain string) (*grpc.ClientConn, error) {
	if !hasPort(host) {
		host = host + ":80"
	}
	if !hasPort(domain) {
		domain = domain + ":80"
	}

	if host != domain {
		// The host to connect and the domain accepted differ.
		// We need to do grpc.WithAuthority(...) here.
		return grpc.Dial(
			host,
			grpc.WithAuthority(domain),
			grpc.WithInsecure(),
			// Retrying DNS errors to avoid .xip.io issues.
			grpc.WithDefaultCallOptions(grpc.FailFast(false)),
		)
	}
	// This is a more preferred usage of the go-grpc client.
	return grpc.Dial(
		host,
		grpc.WithInsecure(),
		// Retrying DNS errors to avoid .xip.io issues.
		grpc.WithDefaultCallOptions(grpc.FailFast(false)),
	)
}

func unaryTest(t *testing.T, resources *test.ResourceObjects, clients *test.Clients, host, domain string) {
	t.Helper()
	t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)
	conn, err := dial(host, domain)
	if err != nil {
		t.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	pc := ping.NewPingServiceClient(conn)
	t.Log("Testing unary Ping")

	want := &ping.Request{Msg: "Hello!"}

	got, err := pc.Ping(context.Background(), want)
	if err != nil {
		t.Fatalf("Couldn't send request: %v", err)
	}

	if got.Msg != want.Msg {
		t.Errorf("Response = %q, want = %q", got.Msg, want.Msg)
	}
}

func streamTest(t *testing.T, resources *test.ResourceObjects, clients *test.Clients, host, domain string) {
	t.Helper()
	t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)
	conn, err := dial(host, domain)
	if err != nil {
		t.Fatalf("Fail to dial: %v", err)
	}
	defer conn.Close()

	pc := ping.NewPingServiceClient(conn)
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

		want := "This is a short message!"

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
			t.Errorf("Stream %d: response = %q, want = %q", i, got, want)
		}
	}

	stream.CloseSend()

	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
}

func testGRPC(t *testing.T, f grpcTest) {
	t.Helper()
	t.Parallel()
	// Setup
	clients := Setup(t)

	t.Log("Creating service for grpc-ping")

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "grpc-ping",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)
	resources, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{
		ContainerPorts: []corev1.ContainerPort{{
			Name:          "h2c",
			ContainerPort: 8080,
		}},
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	domain := resources.Route.Status.URL.Host

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"gRPCPingReadyToServe",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't return success: %v", names.Route, domain, err)
	}

	host := &domain
	if !test.ServingFlags.ResolvableDomain {
		host = &pkgTest.Flags.IngressEndpoint
		if pkgTest.Flags.IngressEndpoint == "" {
			host, err = ingress.GetIngressEndpoint(clients.KubeClient.Kube)
			if err != nil {
				t.Fatalf("Could not get service endpoint: %v", err)
			}
		}
	}

	f(t, resources, clients, *host, domain)
}

func TestGRPCUnaryPing(t *testing.T) {
	testGRPC(t, unaryTest)
}

func TestGRPCStreamingPing(t *testing.T) {
	testGRPC(t, streamTest)
}

func TestGRPCUnaryPingFromZero(t *testing.T) {
	testGRPC(t, func(t *testing.T, resources *test.ResourceObjects, clients *test.Clients, host, domain string) {
		if err := WaitForScaleToZero(t, resourcenames.Deployment(resources.Revision), clients); err != nil {
			t.Fatalf("Could not scale to zero: %v", err)
		}

		unaryTest(t, resources, clients, host, domain)
	})
}

func TestGRPCStreamingPingFromZero(t *testing.T) {
	testGRPC(t, func(t *testing.T, resources *test.ResourceObjects, clients *test.Clients, host, domain string) {
		if err := WaitForScaleToZero(t, resourcenames.Deployment(resources.Revision), clients); err != nil {
			t.Fatalf("Could not scale to zero: %v", err)
		}

		streamTest(t, resources, clients, host, domain)
	})
}
