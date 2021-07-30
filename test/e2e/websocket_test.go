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
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	wsServerTestImageName = "wsserver"
)

const message = "Hello, websocket"

func validateWebSocketConnection(t *testing.T, clients *test.Clients, names test.ResourceNames) error {
	t.Helper()
	// Establish the websocket connection.
	conn, err := connect(t, clients, names.URL.Hostname())
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send a message.
	t.Logf("Sending message %q to server.", message)
	if err = conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
		return err
	}
	t.Log("Message sent.")

	// Read back the echoed message and compared with sent.
	_, recv, err := conn.ReadMessage()
	if err != nil {
		return err
	} else if strings.HasPrefix(string(recv), message) {
		t.Logf("Received message %q from echo server.", recv)
		return nil
	}
	return fmt.Errorf("expected to receive back the message: %q but received %q", message, string(recv))
}

// Connects to a WebSocket target and executes `numReqs` requests.
// Collects the answer frequences and returns them.
// Returns nil map and error if any of the requests fails.
func webSocketResponseFreqs(t *testing.T, clients *test.Clients, url string, numReqs int) (map[string]int, error) {
	t.Helper()
	var g errgroup.Group
	respCh := make(chan string, numReqs)
	resps := map[string]int{}
	for i := 0; i < numReqs; i++ {
		g.Go(func() error {
			// Establish the websocket connection. Since they are persistent
			// we can't reuse.
			conn, err := connect(t, clients, url)
			if err != nil {
				return err
			}
			defer conn.Close()

			// Send a message.
			t.Logf("Sending message %q to server.", message)
			if err = conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
				return err
			}
			t.Log("Message sent.")

			// Read back the echoed message and put it into the channel.
			_, recv, err := conn.ReadMessage()
			if err != nil {
				return err
			}
			respCh <- string(recv)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(respCh)
	for r := range respCh {
		resps[r]++
	}

	return resps, nil
}

// TestWebSocket
// (1) creates a service based on the `wsserver` image,
// (2) connects to the service using websocket,
// (3) sends a message, and
// (4) verifies that we receive back the same message.
func TestWebSocket(t *testing.T) {
	// TODO: https option with parallel leads to flakes.
	// https://github.com/knative/serving/issues/11387
	if !test.ServingFlags.HTTPS {
		t.Parallel()
	}

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   wsServerTestImageName,
	}

	// Clean up in both abnormal and normal exits.
	test.EnsureTearDown(t, clients, &names)

	if _, err := v1test.CreateServiceReady(t, clients, &names); err != nil {
		t.Fatal("Failed to create WebSocket server:", err)
	}

	// Validate the websocket connection.
	if err := validateWebSocketConnection(t, clients, names); err != nil {
		t.Error(err)
	}
}

// and with -1 as target burst capacity and then validates that we can still serve.
func TestWebSocketViaActivator(t *testing.T) {
	// TODO: https option with parallel leads to flakes.
	// https://github.com/knative/serving/issues/11387
	if !test.ServingFlags.HTTPS {
		t.Parallel()
	}

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   wsServerTestImageName,
	}

	// Clean up in both abnormal and normal exits.
	test.EnsureTearDown(t, clients, &names)

	resources, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
	)
	if err != nil {
		t.Fatal("Failed to create WebSocket server:", err)
	}

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(&TestContext{
		t:         t,
		logf:      t.Logf,
		resources: resources,
		clients:   clients,
	}); err != nil {
		t.Fatal("Never got Activator endpoints in the service:", err)
	}
	if err := validateWebSocketConnection(t, clients, names); err != nil {
		t.Error(err)
	}
}

func TestWebSocketBlueGreenRoute(t *testing.T) {
	// TODO: https option with parallel leads to flakes.
	// https://github.com/knative/serving/issues/11387
	if !test.ServingFlags.HTTPS {
		t.Parallel()
	}
	clients := test.Setup(t)

	svcName := test.ObjectNameForTest(t)
	// Long name hits this issue https://github.com/knative-sandbox/net-certmanager/issues/214
	if test.ServingFlags.HTTPS {
		svcName = test.AppendRandomString("web-socket-blue-green")
	}

	names := test.ResourceNames{
		// Set Service and Image for names to create the initial service
		Service: svcName,
		Image:   wsServerTestImageName,
	}

	test.EnsureTearDown(t, clients, &names)

	// Setup Initial Service
	t.Log("Creating a new Service in runLatest")
	objects, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithEnv(corev1.EnvVar{
			Name:  "SUFFIX",
			Value: "Blue",
		}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	blue := names
	blue.TrafficTarget = "blue"

	t.Log("Updating the Service to use a different suffix")
	greenSvc, err := v1test.PatchService(t, clients, objects.Service, func(s *v1.Service) {
		s.Spec.Template.Spec.Containers[0].Env[0].Value = "Green"
	})
	if err != nil {
		t.Fatalf("Patch update for Service %s with new env var failed: %v", names.Service, err)
	}
	objects.Service = greenSvc
	green := names
	green.TrafficTarget = "green"

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	green.Revision, err = v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new Revision: %v", names.Service, err)
	}

	t.Log("Updating RouteSpec")
	if _, err := v1test.PatchServiceRouteSpec(t, clients, names, v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:          blue.TrafficTarget,
			RevisionName: blue.Revision,
			Percent:      ptr.Int64(50),
		}, {
			Tag:          green.TrafficTarget,
			RevisionName: green.Revision,
			Percent:      ptr.Int64(50),
		}},
	}); err != nil {
		t.Fatal("Failed to update Service route spec:", err)
	}

	t.Log("Wait for the service domains to be ready")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	service, err := clients.ServingClient.Services.Get(context.Background(), names.Service, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error fetching Service %s: %v", names.Service, err)
	}

	// Update the names
	for _, tt := range service.Status.Traffic {
		if tt.Tag == green.TrafficTarget {
			green.URL = tt.URL.URL()
		}
	}
	if green.URL == nil {
		t.Fatalf("Unable to fetch Green URL from traffic targets: %#v", service.Status.Traffic)
	}
	// Since we just validate that Network layer can properly route requests to different targets
	// We'll just use the service URL.
	tealURL := service.Status.URL.URL().Hostname()

	// But since network programming takes some time to take effect
	// and it doesn't have a Status, we'll probe `green` until it's ready first.
	if err := validateWebSocketConnection(t, clients, green); err != nil {
		t.Fatal("Error initializing WS connection:", err)
	}

	// The actual test.
	const (
		numReqs = 200
		// Quite high, but makes sure we didn't get a one-off successful response from either target.
		tolerance = 25
	)
	resps, err := webSocketResponseFreqs(t, clients, tealURL, numReqs)
	if err != nil {
		t.Error("Failed to send and receive websocket messages:", err)
	}
	if len(resps) != 2 {
		t.Errorf("Number of responses: %d, want: 2", len(resps))
	}
	for k, f := range resps {
		if got, want := abs(f-numReqs/2), tolerance; got > want {
			t.Errorf("Target %s got %d responses, expect in [%d, %d] interval", k, f, numReqs/2-tolerance, numReqs/2+tolerance)
		}
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
