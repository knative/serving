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
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	ping "knative.dev/serving/test/test_images/grpc-ping/proto"
)

// TestGRPC verifies that GRPC may be used via a simple Ingress.
func TestGRPC(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	const suffix = "- pong"
	name, port, cancel := CreateGRPCService(t, clients, suffix)
	defer cancel()

	domain := name + ".example.com"

	// Create a simple Ingress over the Service.
	_, dialCtx, cancel := CreateIngressReadyDialContext(t, clients, v1alpha1.IngressSpec{
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
	defer cancel()

	conn, err := grpc.Dial(
		domain+":80",
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialCtx(ctx, "unused", addr)
		}),
	)
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	defer conn.Close()
	pc := ping.NewPingServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := pc.PingStream(ctx)
	if err != nil {
		t.Fatalf("PingStream() = %v", err)
	}

	for i := 0; i < 100; i++ {
		message := fmt.Sprintf("ping - %d", rand.Intn(1000))
		err = stream.Send(&ping.Request{Msg: message})
		if err != nil {
			t.Fatalf("Error sending request: %v", err)
		}

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Error receiving response: %v", err)
		}

		if got, want := resp.Msg, message+suffix; got != want {
			t.Errorf("ReadMessage() = %s, wanted %s", got, want)
		}
	}
}

// TestGRPCSplit verifies that websockets may be used across a traffic split.
func TestGRPCSplit(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	const suffixBlue = "- blue"
	blueName, bluePort, cancel := CreateGRPCService(t, clients, suffixBlue)
	defer cancel()

	const suffixGreen = "- green"
	greenName, greenPort, cancel := CreateGRPCService(t, clients, suffixGreen)
	defer cancel()

	// The suffixes we expect to see.
	want := sets.NewString(suffixBlue, suffixGreen)

	// Create a simple Ingress over the Service.
	name := test.ObjectNameForTest(t)
	domain := name + ".example.com"
	_, dialCtx, cancel := CreateIngressReadyDialContext(t, clients, v1alpha1.IngressSpec{
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
	defer cancel()

	conn, err := grpc.Dial(
		domain+":80",
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialCtx(ctx, "unused", addr)
		}),
	)
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	defer conn.Close()
	pc := ping.NewPingServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	got := sets.NewString()
	for i := 0; i < 10; i++ {
		stream, err := pc.PingStream(ctx)
		if err != nil {
			t.Fatalf("PingStream() = %v", err)
		}

		message := fmt.Sprintf("ping - %d", rand.Intn(1000))
		err = stream.Send(&ping.Request{Msg: message})
		if err != nil {
			t.Fatalf("Error sending request: %v", err)
		}

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Error receiving response: %v", err)
		}
		gotMsg := resp.Msg
		if !strings.HasPrefix(gotMsg, message) {
			t.Errorf("Recv() = %s, wanted %s prefix", got, message)
			continue
		}
		suffix := strings.TrimSpace(strings.TrimPrefix(gotMsg, message))
		got.Insert(suffix)

		for j := 0; j < 10; j++ {
			message := fmt.Sprintf("ping - %d", rand.Intn(1000))
			err = stream.Send(&ping.Request{Msg: message})
			if err != nil {
				t.Fatalf("Error sending request: %v", err)
			}

			resp, err := stream.Recv()
			if err != nil {
				t.Fatalf("Error receiving response: %v", err)
			}

			if got, want := resp.Msg, message+suffix; got != want {
				t.Errorf("ReadMessage() = %s, wanted %s", got, want)
			}
		}
	}

	if !cmp.Equal(want, got) {
		t.Errorf("(-want, +got) = %s", cmp.Diff(want, got))
	}
}
