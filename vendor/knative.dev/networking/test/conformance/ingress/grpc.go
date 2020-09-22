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

package ingress

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	ping "knative.dev/networking/test/test_images/grpc-ping/proto"
)

// TestGRPC verifies that GRPC may be used via a simple Ingress.
func TestGRPC(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	const suffix = "- pong"
	name, port, _ := CreateGRPCService(ctx, t, clients, suffix)

	domain := name + ".example.com"

	// Create a simple Ingress over the Service.
	_, dialCtx, _ := createIngressReadyDialContext(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{domain},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      name,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
					}},
				}},
			},
		}},
	})

	conn, err := grpc.Dial(
		domain+":80",
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialCtx(ctx, "unused", addr)
		}),
	)
	if err != nil {
		t.Fatal("Dial() =", err)
	}
	defer conn.Close()
	pc := ping.NewPingServiceClient(conn)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	stream, err := pc.PingStream(ctx)
	if err != nil {
		t.Fatal("PingStream() =", err)
	}

	for i := 0; i < 100; i++ {
		checkGRPCRoundTrip(t, stream, suffix)
	}
}

// TestGRPCSplit verifies that websockets may be used across a traffic split.
func TestGRPCSplit(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	const suffixBlue = "- blue"
	blueName, bluePort, _ := CreateGRPCService(ctx, t, clients, suffixBlue)

	const suffixGreen = "- green"
	greenName, greenPort, _ := CreateGRPCService(ctx, t, clients, suffixGreen)

	// The suffixes we expect to see.
	want := sets.NewString(suffixBlue, suffixGreen)

	// Create a simple Ingress over the Service.
	name := test.ObjectNameForTest(t)
	domain := name + ".example.com"
	_, dialCtx, _ := createIngressReadyDialContext(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{domain},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      blueName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(bluePort),
						},
						Percent: 50,
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      greenName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(greenPort),
						},
						Percent: 50,
					}},
				}},
			},
		}},
	})

	conn, err := grpc.Dial(
		domain+":80",
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialCtx(ctx, "unused", addr)
		}),
	)
	if err != nil {
		t.Fatal("Dial() =", err)
	}
	defer conn.Close()
	pc := ping.NewPingServiceClient(conn)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	const maxRequests = 100
	got := sets.NewString()
	for i := 0; i < maxRequests; i++ {
		stream, err := pc.PingStream(ctx)
		if err != nil {
			t.Errorf("PingStream() = %v", err)
			continue
		}

		suffix := findGRPCSuffix(t, stream)
		if suffix == "" {
			continue
		}
		got.Insert(suffix)

		for j := 0; j < 10; j++ {
			checkGRPCRoundTrip(t, stream, suffix)
		}

		if want.Equal(got) {
			// Short circuit if we've seen all splits.
			return
		}
	}

	// Us getting here means we haven't seen splits.
	t.Errorf("(over %d requests) (-want, +got) = %s", maxRequests, cmp.Diff(want, got))
}

func findGRPCSuffix(t *testing.T, stream ping.PingService_PingStreamClient) string {
	// Establish the suffix that corresponds to this stream.
	message := fmt.Sprintf("ping - %d", rand.Intn(1000))
	if err := stream.Send(&ping.Request{Msg: message}); err != nil {
		t.Errorf("Error sending request: %v", err)
		return ""
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Errorf("Error receiving response: %v", err)
		return ""
	}
	gotMsg := resp.Msg
	if !strings.HasPrefix(gotMsg, message) {
		t.Errorf("Recv() = %s, wanted %s prefix", gotMsg, message)
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(gotMsg, message))
}

func checkGRPCRoundTrip(t *testing.T, stream ping.PingService_PingStreamClient, suffix string) {
	message := fmt.Sprintf("ping - %d", rand.Intn(1000))
	if err := stream.Send(&ping.Request{Msg: message}); err != nil {
		t.Errorf("Error sending request: %v", err)
		return
	}

	// Read back the echoed message and compared with sent.
	if resp, err := stream.Recv(); err != nil {
		t.Errorf("Error receiving response: %v", err)
	} else if got, want := resp.Msg, message+suffix; got != want {
		t.Errorf("Recv() = %s, wanted %s", got, want)
	}
}
