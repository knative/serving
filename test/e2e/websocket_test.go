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
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	connectRetryInterval = 1 * time.Second
	connectTimeout       = 6 * time.Minute
)

// connect attempts to establish WebSocket connection with the Service.
// It will retry until reaching `connectTimeout` duration.
func connect(t *testing.T, ingressIP string, domain string) (*websocket.Conn, error) {
	u := url.URL{Scheme: "ws", Host: ingressIP, Path: "/"}
	var conn *websocket.Conn
	waitErr := wait.PollImmediate(connectRetryInterval, connectTimeout, func() (bool, error) {
		t.Logf("Connecting using websocket: url=%s, host=%s", u.String(), domain)
		c, resp, err := websocket.DefaultDialer.Dial(u.String(), http.Header{"Host": []string{domain}})
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

// While we do have similar logic in knative/pkg, it is deeply buried
// inside the SpoofClient which is very HTTP centric.
//
// TODO(tcnghia): Extract the GatewayIP logic out from SpoofClient.
// Also, we should deduce this information from the child
// ClusterIngress's Status.
func getGatewayIP(kube *kubernetes.Clientset) (string, error) {
	const ingressName = "istio-ingressgateway"
	const ingressNamespace = "istio-system"

	ingress, err := kube.CoreV1().Services(ingressNamespace).Get(ingressName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if len(ingress.Status.LoadBalancer.Ingress) != 1 {
		return "", fmt.Errorf("expected exactly one ingress load balancer, instead had %d: %s",
			len(ingress.Status.LoadBalancer.Ingress), ingress.Status.LoadBalancer.Ingress)
	}
	if ingress.Status.LoadBalancer.Ingress[0].IP == "" {
		return "", fmt.Errorf("expected ingress loadbalancer IP for %s to be set, instead was empty", ingressName)
	}
	return ingress.Status.LoadBalancer.Ingress[0].IP, nil
}

func validateWebSocketConnection(t *testing.T, clients *test.Clients, names test.ResourceNames) error {
	// Get the gatewayIP.
	gatewayIP, err := getGatewayIP(clients.KubeClient.Kube)
	if err != nil {
		return err
	}

	// Establish the websocket connection.
	conn, err := connect(t, gatewayIP, names.Domain)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send a message.
	const sent = "Hello, websocket"
	t.Logf("Sending message %q to server.", sent)
	if err = conn.WriteMessage(websocket.TextMessage, []byte(sent)); err != nil {
		return err
	}
	t.Log("Message sent.")

	// Read back the echoed message and compared with sent.
	if _, recv, err := conn.ReadMessage(); err != nil {
		return err
	} else if sent != string(recv) {
		return fmt.Errorf("expected to receive back the message: %q but received %q", sent, string(recv))
	} else {
		t.Logf("Received message %q from echo server.", recv)
	}
	return nil
}

// TestWebSocket (1) creates a service based on the `wsserver` image,
// (2) connects to the service using websocket, (3) sends a message, and
// (4) verifies that we receive back the same message.
func TestWebSocket(t *testing.T) {
	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "wsserver",
	}

	// Clean up in both abnormal and normal exits.
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	// Setup a WebSocket server.
	if _, err := test.CreateRunLatestServiceReady(t, clients, &names, &test.Options{}); err != nil {
		t.Fatalf("Failed to create WebSocket server: %v", err)
	}

	// Validate the websocket connection.
	if err := validateWebSocketConnection(t, clients, names); err != nil {
		t.Error(err)
	}
}
