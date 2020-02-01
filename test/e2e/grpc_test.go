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
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	ping "knative.dev/serving/test/test_images/grpc-ping/proto"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

const (
	grpcContainerConcurrency = 1.0
)

type grpcTest func(*testing.T, *v1a1test.ResourceObjects, *test.Clients, test.ResourceNames, string, string)

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
			grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		)
	}
	// This is a more preferred usage of the go-grpc client.
	return grpc.Dial(
		host,
		grpc.WithInsecure(),
		// Retrying DNS errors to avoid .xip.io issues.
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
}

func unaryTest(t *testing.T, resources *v1a1test.ResourceObjects, clients *test.Clients, names test.ResourceNames, host, domain string) {
	t.Helper()
	t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)
	msg := "Hello!"
	pingGRPC(host, domain, msg)
}

func autoscaleTest(t *testing.T, resources *v1a1test.ResourceObjects, clients *test.Clients, names test.ResourceNames, host, domain string) {
	t.Helper()
	t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)

	ctx := &testContext{
		t:                 t,
		clients:           clients,
		resources:         resources,
		names:             names,
		targetUtilization: targetUtilization,
	}
	assertGRPCAutoscaleUpToNumPods(ctx, 1, 2, 60*time.Second, host, domain)
	assertScaleDown(ctx)
	assertGRPCAutoscaleUpToNumPods(ctx, 0, 2, 60*time.Second, host, domain)
}

func generateGRPCTraffic(concurrentRequests int, host, domain string, stopChan chan struct{}) error {
	var grp errgroup.Group

	for i := 0; i < concurrentRequests; i++ {
		i := i
		grp.Go(func() error {
			for j := 0; ; j++ {
				select {
				case <-stopChan:
					return nil
				default:
					msg := fmt.Sprintf("Hello! stream:%d request: %d", i, j)
					if err := pingGRPC(host, domain, msg); err != nil {
						return err
					}
				}
			}
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		return fmt.Errorf("Error processing requests %v", err)
	}
	return nil
}

func pingGRPC(host, domain, message string) error {
	conn, err := dial(host, domain)
	if err != nil {
		return err
	}
	defer conn.Close()

	pc := ping.NewPingServiceClient(conn)
	want := &ping.Request{Msg: message}

	got, err := pc.Ping(context.Background(), want)
	if err != nil {
		return fmt.Errorf("Could not send request: %v", err)
	}

	if got.Msg != want.Msg {
		return fmt.Errorf("Response = %q, want = %q", got.Msg, want.Msg)
	}
	return nil
}

func assertGRPCAutoscaleUpToNumPods(ctx *testContext, curPods, targetPods float64, duration time.Duration, host, domain string) {
	ctx.t.Helper()
	// Test succeeds when the number of pods meets targetPods.

	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	// Also adjust the values by the target utilization values.

	minPods := math.Floor(curPods/ctx.targetUtilization) - 1
	maxPods := math.Ceil(targetPods/ctx.targetUtilization) + 1

	stopChan := make(chan struct{})
	var grp errgroup.Group

	grp.Go(func() error {
		return generateGRPCTraffic(int(targetPods*grpcContainerConcurrency), host, domain, stopChan)
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, duration)
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Errorf("Error : %v", err)
	}
}

func streamTest(t *testing.T, resources *v1a1test.ResourceObjects, clients *test.Clients, names test.ResourceNames, host, domain string) {
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

func testGRPC(t *testing.T, f grpcTest, fopts ...rtesting.ServiceOption) {
	t.Helper()
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	// Setup
	clients := Setup(t)

	t.Log("Creating service for grpc-ping")

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "grpc-ping",
	}

	fopts = append(fopts, rtesting.WithNamedPort("h2c"))

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)
	resources, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		fopts...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	url := resources.Route.Status.URL.URL()

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"gRPCPingReadyToServe",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
	}

	host := url.Host
	if !test.ServingFlags.ResolvableDomain {
		host = pkgTest.Flags.IngressEndpoint
		if pkgTest.Flags.IngressEndpoint == "" {
			host, err = ingress.GetIngressEndpoint(clients.KubeClient.Kube)
			if err != nil {
				t.Fatalf("Could not get service endpoint: %v", err)
			}
		}
	}

	f(t, resources, clients, names, host, url.Hostname())
}

func TestGRPCUnaryPing(t *testing.T) {
	testGRPC(t, unaryTest)
}

func TestGRPCStreamingPing(t *testing.T) {
	testGRPC(t, streamTest)
}

func TestGRPCUnaryPingViaActivator(t *testing.T) {
	testGRPC(t,
		func(t *testing.T, resources *v1a1test.ResourceObjects, clients *test.Clients, names test.ResourceNames, host, domain string) {
			if err := waitForActivatorEndpoints(resources, clients); err != nil {
				t.Fatalf("Never got Activator endpoints in the service: %v", err)
			}
			unaryTest(t, resources, clients, names, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
	)
}

func TestGRPCStreamingPingViaActivator(t *testing.T) {
	testGRPC(t,
		func(t *testing.T, resources *v1a1test.ResourceObjects, clients *test.Clients, names test.ResourceNames, host, domain string) {
			if err := waitForActivatorEndpoints(resources, clients); err != nil {
				t.Fatalf("Never got Activator endpoints in the service: %v", err)
			}
			streamTest(t, resources, clients, names, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
	)
}

func TestGRPCAutoscaleUpDownUp(t *testing.T) {
	testGRPC(t,
		func(t *testing.T, resources *v1a1test.ResourceObjects, clients *test.Clients, names test.ResourceNames, host, domain string) {
			autoscaleTest(t, resources, clients, names, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetUtilizationPercentageKey: strconv.FormatFloat(targetUtilization*100, 'f', -1, 64),
			autoscaling.TargetAnnotationKey:            strconv.FormatFloat(grpcContainerConcurrency, 'f', -1, 64),
			autoscaling.TargetBurstCapacityKey:         strconv.FormatFloat(-1, 'f', -1, 64),
			autoscaling.WindowAnnotationKey:            "10s",
		}),
		rtesting.WithEnv(corev1.EnvVar{
			Name:  "DELAY",
			Value: "500",
		}),
	)
}
