//go:build e2e
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
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	corev1 "k8s.io/api/core/v1"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	ping "knative.dev/serving/test/test_images/grpc-ping/proto"
	v1test "knative.dev/serving/test/v1"
)

const (
	grpcContainerConcurrency = 1
	grpcMinScale             = 3
)

type grpcTest func(*TestContext, string, string)

// hasPort checks if a URL contains a port number
func hasPort(u string) bool {
	_, port, err := net.SplitHostPort(u)
	if err != nil {
		return false
	}
	_, err = strconv.Atoi(port)
	return err == nil
}

func dial(ctx *TestContext, host, domain string) (*grpc.ClientConn, error) {
	defaultPort := "80"
	if !hasPort(host) {
		host = net.JoinHostPort(host, defaultPort)
	}
	if hasPort(domain) {
		var err error
		domain, _, err = net.SplitHostPort(domain)
		if err != nil {
			return nil, err
		}
	}

	creds := insecure.NewCredentials()

	return grpc.Dial(
		host,
		grpc.WithAuthority(domain),
		grpc.WithTransportCredentials(creds),
		// Retrying DNS errors to avoid .sslip.io issues.
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
}

func unaryTest(ctx *TestContext, host, domain string) {
	ctx.t.Helper()
	ctx.t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)
	const want = "Hello!"
	got, err := pingGRPC(ctx, host, domain, want)
	if err != nil {
		ctx.t.Fatal("gRPC ping =", err)
	}
	if got != want {
		ctx.t.Fatalf("Response = %q, want = %q", got, want)
	}
}

func autoscaleTest(ctx *TestContext, host, domain string) {
	ctx.t.Helper()
	ctx.t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)

	assertGRPCAutoscaleUpToNumPods(ctx, 1, 2, 60*time.Second, host, domain)
	assertScaleDown(ctx)
	assertGRPCAutoscaleUpToNumPods(ctx, 0, 2, 60*time.Second, host, domain)
}

func loadBalancingTest(ctx *TestContext, host, domain string) {
	ctx.t.Helper()
	ctx.t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)

	const (
		wantHosts  = grpcMinScale
		wantPrefix = "hello-"
	)

	var (
		grp         errgroup.Group
		uniqueHosts sync.Map
		stopChan    = make(chan struct{})
		done        = time.After(60 * time.Second)
		timer       = time.Tick(1 * time.Second)
	)

	countKeys := func() int {
		count := 0
		uniqueHosts.Range(func(k, v interface{}) bool {
			count++
			return true
		})
		return count
	}

	for i := 0; i < wantHosts; i++ {
		grp.Go(func() error {
			for {
				select {
				case <-stopChan:
					return nil
				default:
					got, err := pingGRPC(ctx, host, domain, wantPrefix)
					if err != nil {
						return fmt.Errorf("ping gRPC error: %w", err)
					}
					if !strings.HasPrefix(got, wantPrefix) {
						return fmt.Errorf("response = %q, wantPrefix = %q", got, wantPrefix)
					}

					if host := strings.TrimPrefix(got, wantPrefix); host != "" {
						uniqueHosts.Store(host, true)
					}
				}
			}
		})
	}

	grp.Go(func() error {
		defer close(stopChan)
		for {
			select {
			case <-done:
				return nil
			case <-timer:
				if countKeys() >= wantHosts {
					return nil
				}
			}
		}
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Fatal("error: ", err)
	}

	gotHosts := countKeys()
	if gotHosts < wantHosts {
		ctx.t.Fatalf("Wanted %d hosts, got %d hosts", wantHosts, gotHosts)
	}
}

func generateGRPCTraffic(ctx *TestContext, concurrentRequests int, host, domain string, stopChan chan struct{}) error {
	var grp errgroup.Group

	for i := 0; i < concurrentRequests; i++ {
		i := i
		grp.Go(func() error {
			for j := 0; ; j++ {
				select {
				case <-stopChan:
					return nil
				default:
					want := fmt.Sprintf("Hello! stream:%d request: %d", i, j)
					got, err := pingGRPC(ctx, host, domain, want)

					if err != nil {
						return fmt.Errorf("ping gRPC error: %w", err)
					}
					if got != want {
						return fmt.Errorf("response = %q, want = %q", got, want)
					}
				}
			}
		})
	}
	if err := grp.Wait(); err != nil {
		return fmt.Errorf("error processing requests %w", err)
	}
	return nil
}

func pingGRPC(ctx *TestContext, host, domain, message string) (string, error) {
	conn, err := dial(ctx, host, domain)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	pc := ping.NewPingServiceClient(conn)
	want := &ping.Request{Msg: message}

	got, err := pc.Ping(context.Background(), want)
	if err != nil {
		return "", fmt.Errorf("could not send request: %w", err)
	}
	return got.Msg, nil
}

func assertGRPCAutoscaleUpToNumPods(ctx *TestContext, curPods, targetPods float64, duration time.Duration, host, domain string) {
	ctx.t.Helper()
	// Test succeeds when the number of pods meets targetPods.

	// Relax the bounds to reduce the flakiness caused by sampling in the autoscaling algorithm.
	// Also adjust the values by the target utilization values.

	minPods := math.Floor(curPods/ctx.autoscaler.TargetUtilization) - 1
	maxPods := math.Ceil(targetPods/ctx.autoscaler.TargetUtilization) + 1

	stopChan := make(chan struct{})
	var grp errgroup.Group

	grp.Go(func() error {
		return generateGRPCTraffic(ctx, int(targetPods*float64(ctx.autoscaler.Target)), host, domain, stopChan)
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, time.After(duration), true /* quick */)
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Errorf("Error : %v", err)
	}
}

func streamTest(tc *TestContext, host, domain string) {
	tc.t.Helper()
	tc.t.Logf("Connecting to grpc-ping using host %q and authority %q", host, domain)
	conn, err := dial(tc, host, domain)
	if err != nil {
		tc.t.Fatal("Fail to dial:", err)
	}
	defer conn.Close()

	pc := ping.NewPingServiceClient(conn)
	tc.t.Log("Testing streaming Ping")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := pc.PingStream(ctx)
	if err != nil {
		tc.t.Fatal("Error creating stream:", err)
	}

	const count = 3
	for i := 0; i < count; i++ {
		tc.t.Logf("Sending stream %d of %d", i+1, count)

		want := "This is a short message!"

		err = stream.Send(&ping.Request{Msg: want})
		if err != nil {
			tc.t.Fatal("Error sending request:", err)
		}

		resp, err := stream.Recv()
		if err != nil {
			tc.t.Fatal("Error receiving response:", err)
		}

		got := resp.Msg

		if want != got {
			tc.t.Errorf("Stream %d: response = %q, want = %q", i, got, want)
		}
	}

	stream.CloseSend()

	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		tc.t.Errorf("Expected EOF, got %v", err)
	}
}

func testGRPC(t *testing.T, f grpcTest, fopts ...rtesting.ServiceOption) {
	t.Helper()

	// Setup
	clients := Setup(t)

	t.Log("Creating service for grpc-ping")

	svcName := test.ObjectNameForTest(t)
	// Long name hits this issue https://github.com/knative-extensions/net-certmanager/issues/214
	if t.Name() == "TestGRPCStreamingPingViaActivator" {
		svcName = test.AppendRandomString("grpc-streaming-pig-act")
	}

	names := &test.ResourceNames{
		Service: svcName,
		Image:   test.GRPCPing,
	}

	fopts = append(fopts, rtesting.WithNamedPort("h2c"))

	test.EnsureTearDown(t, clients, names)
	resources, err := v1test.CreateServiceReady(t, clients, names, fopts...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	url := resources.Route.Status.URL.URL()

	if _, err = pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.IsStatusOK,
		"gRPCPingReadyToServe",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint for Route %s at %s didn't return success: %v", names.Route, url, err)
	}

	host := url.Host
	if true {
		addr, mapper, err := ingress.GetIngressEndpoint(context.Background(), clients.KubeClient, pkgTest.Flags.IngressEndpoint)
		if err != nil {
			t.Fatal("Could not get service endpoint:", err)
		}

		host = net.JoinHostPort(addr, mapper("80"))
	}

	f(&TestContext{
		t:         t,
		clients:   clients,
		names:     names,
		resources: resources,
	}, host, url.Hostname())
}

func TestGRPCUnaryPing(t *testing.T) {
	testGRPC(t, unaryTest)
}

func TestGRPCStreamingPing(t *testing.T) {
	testGRPC(t, streamTest)
}

func TestGRPCUnaryPingViaActivator(t *testing.T) {
	testGRPC(t,
		func(ctx *TestContext, host, domain string) {
			if err := waitForActivatorEndpoints(ctx); err != nil {
				t.Fatal("Never got Activator endpoints in the service:", err)
			}
			unaryTest(ctx, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
	)
}

func TestGRPCStreamingPingViaActivator(t *testing.T) {
	testGRPC(t,
		func(ctx *TestContext, host, domain string) {
			if err := waitForActivatorEndpoints(ctx); err != nil {
				t.Fatal("Never got Activator endpoints in the service:", err)
			}
			streamTest(ctx, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
	)
}

func TestGRPCAutoscaleUpDownUp(t *testing.T) {
	aOpts := &AutoscalerOptions{
		TargetUtilization: targetUtilization,
		Target:            grpcContainerConcurrency,
	}
	testGRPC(t,
		func(ctx *TestContext, host, domain string) {
			ctx.autoscaler = aOpts
			autoscaleTest(ctx, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetUtilizationPercentageKey: toPercentageString(aOpts.TargetUtilization),
			autoscaling.TargetAnnotationKey:            strconv.Itoa(aOpts.Target),
			autoscaling.TargetBurstCapacityKey:         "-1",
			autoscaling.WindowAnnotationKey:            "10s",
		}),
		rtesting.WithEnv(corev1.EnvVar{
			Name:  "DELAY",
			Value: "500",
		}),
	)
}

func TestGRPCLoadBalancing(t *testing.T) {
	aOpts := &AutoscalerOptions{
		TargetUtilization: targetUtilization,
		Target:            grpcContainerConcurrency,
	}
	testGRPC(t,
		func(ctx *TestContext, host, domain string) {
			ctx.autoscaler = aOpts
			loadBalancingTest(ctx, host, domain)
		},
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetUtilizationPercentageKey: toPercentageString(aOpts.TargetUtilization),
			autoscaling.TargetAnnotationKey:            strconv.Itoa(aOpts.Target),
			autoscaling.MinScaleAnnotationKey:          strconv.Itoa(grpcMinScale),
			autoscaling.TargetBurstCapacityKey:         "-1",
		}),
		rtesting.WithEnv(corev1.EnvVar{
			Name:  "HOSTNAME",
			Value: "true",
		}),
	)
}
