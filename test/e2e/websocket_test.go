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
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	ingress "knative.dev/pkg/test/ingress"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	rtesting "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

const (
	connectRetryInterval  = 1 * time.Second
	connectTimeout        = 1 * time.Minute
	wsServerTestImageName = "wsserver"
)

// connect attempts to establish WebSocket connection with the Service.
// It will retry until reaching `connectTimeout` duration.
func connect(t *testing.T, clients *test.Clients, domain string) (*websocket.Conn, error) {
	var (
		err     error
		address string
	)

	if test.ServingFlags.ResolvableDomain {
		address = domain
	} else if pkgTest.Flags.IngressEndpoint != "" {
		address = pkgTest.Flags.IngressEndpoint
	} else if address, err = ingress.GetIngressEndpoint(clients.KubeClient.Kube); err != nil {
		return nil, err
	}

	u := url.URL{Scheme: "ws", Host: address, Path: "/"}
	var conn *websocket.Conn
	waitErr := wait.PollImmediate(connectRetryInterval, connectTimeout, func() (bool, error) {
		t.Logf("Connecting using websocket: url=%s, host=%s", u.String(), domain)
		c, resp, err := websocket.DefaultDialer.Dial(u.String(), http.Header{"Host": {domain}})
		if err == nil {
			t.Log("WebSocket connection established.")
			conn = c
			return true, nil
		}
		if resp == nil {
			// We don't have an HTTP response, probably TCP errors.
			t.Logf("Connection failed: %v", err)
			return false, nil
		}
		body := &bytes.Buffer{}
		defer resp.Body.Close()
		if _, readErr := body.ReadFrom(resp.Body); readErr != nil {
			t.Logf("Connection failed: %v. Failed to read HTTP response: %v", err, readErr)
			return false, nil
		}
		t.Logf("HTTP connection failed: %v. Response=%+v. ResponseBody=%q", err, resp, body.String())
		return false, nil
	})
	return conn, waitErr
}

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
			t.Logf("Received message %q from echo server.", string(recv))
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
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   wsServerTestImageName,
	}

	// Clean up in both abnormal and normal exits.
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	if _, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false /* https TODO(taragu) turn this on after helloworld test running with https */); err != nil {
		t.Fatalf("Failed to create WebSocket server: %v", err)
	}

	// Validate the websocket connection.
	if err := validateWebSocketConnection(t, clients, names); err != nil {
		t.Error(err)
	}
}

// and with -1 as target burst capacity and then validates that we can still serve.
func TestWebSocketViaActivator(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   wsServerTestImageName,
	}

	// Clean up in both abnormal and normal exits.
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	resources, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create WebSocket server: %v", err)
	}

	// Wait for the activator endpoints to equalize.
	if err := waitForActivatorEndpoints(resources, clients); err != nil {
		t.Fatalf("Never got Activator endpoints in the service: %v", err)
	}
	if err := validateWebSocketConnection(t, clients, names); err != nil {
		t.Error(err)
	}
}

func TestWebSocketBlueGreenRoute(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		// Set Service and Image for names to create the initial service
		Service: test.ObjectNameForTest(t),
		Image:   wsServerTestImageName,
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	// Setup Initial Service
	t.Log("Creating a new Service in runLatest")
	objects, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with HTTPS */
		func(s *v1alpha1.Service) {
			s.Spec.ConfigurationSpec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{
				Name:  "SUFFIX",
				Value: "Blue",
			}}
		},
	)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	blue := names
	blue.TrafficTarget = "blue"

	t.Log("Updating the Service to use a different suffix")
	greenSvc := objects.Service.DeepCopy()
	greenSvc.Spec.ConfigurationSpec.Template.Spec.Containers[0].Env[0].Value = "Green"
	greenSvc, err = v1a1test.PatchService(t, clients, objects.Service, greenSvc)
	if err != nil {
		t.Fatalf("Patch update for Service %s with new env var failed: %v", names.Service, err)
	}
	objects.Service = greenSvc
	green := names
	green.TrafficTarget = "green"

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	green.Revision, err = v1a1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the new Revision: %v", names.Service, err)
	}

	t.Log("Updating RouteSpec")
	if _, err := v1a1test.UpdateServiceRouteSpec(t, clients, names, v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				Tag:          blue.TrafficTarget,
				RevisionName: blue.Revision,
				Percent:      ptr.Int64(50),
			},
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:          green.TrafficTarget,
				RevisionName: green.Revision,
				Percent:      ptr.Int64(50),
			},
		}},
	}); err != nil {
		t.Fatalf("Failed to update Service route spec: %v", err)
	}

	t.Log("Wait for the service domains to be ready")
	if err := v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, v1a1test.IsServiceReady, "ServiceIsReady"); err != nil {
		t.Fatalf("The Service %s was not marked as Ready to serve traffic: %v", names.Service, err)
	}

	service, err := clients.ServingAlphaClient.Services.Get(names.Service, metav1.GetOptions{})
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

	// But since Istio network programming takes some time to take effect
	// and it doesn't have a Status, we'll probe `green` until it's ready first.
	if err := validateWebSocketConnection(t, clients, green); err != nil {
		t.Fatalf("Error initializing WS connection: %v", err)
	}

	// The actual test.
	const (
		numReqs = 200
		// Quite high, but makes sure we didn't get a one-off successful response from either target.
		tolerance = 25
	)
	resps, err := webSocketResponseFreqs(t, clients, tealURL, numReqs)
	if err != nil {
		t.Errorf("Failed to send and receive websocket messages: %v", err)
	}
	if len(resps) != 2 {
		t.Errorf("Number of responses: %d, want: 2", len(resps))
	}
	for k, f := range resps {
		if got, want := abs(f-numReqs/2), tolerance; got > want {
			t.Errorf("Target %s got %d responses, expect in [%d, %d] interval", k, f, numReqs/2-5, numReqs/2+tolerance)
		}
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
