/*
Copyright 2020 The Knative Authors

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
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"k8s.io/apimachinery/pkg/util/wait"
	pkgTest "knative.dev/pkg/test"
	ingress "knative.dev/pkg/test/ingress"
	"knative.dev/serving/test"
)

const (
	connectRetryInterval = 1 * time.Second
	connectTimeout       = 1 * time.Minute
)

const message = "Hello, websocket"

// connect attempts to establish WebSocket connection with the Service.
// It will retry until reaching `connectTimeout` duration.
func connect(t *testing.T, clients *test.Clients, domain, timeout string) (*websocket.Conn, error) {
	var (
		err     error
		address string
	)

	address = domain
	mapper := func(in string) string { return in }
	if !test.ServingFlags.ResolvableDomain {
		address, mapper, err = ingress.GetIngressEndpoint(context.Background(), clients.KubeClient, pkgTest.Flags.IngressEndpoint)
		if err != nil {
			return nil, err
		}
	}

	rawQuery := fmt.Sprintf("delay=%s", timeout)
	u := url.URL{Scheme: "ws", Host: net.JoinHostPort(address, mapper("80")), Path: "/", RawQuery: rawQuery}
	if test.ServingFlags.HTTPS {
		u = url.URL{Scheme: "wss", Host: net.JoinHostPort(address, mapper("443")), Path: "/", RawQuery: rawQuery}
	}

	var conn *websocket.Conn
	waitErr := wait.PollImmediate(connectRetryInterval, connectTimeout, func() (bool, error) {
		t.Logf("Connecting using websocket: url=%s, host=%s", u.String(), domain)
		dialer := &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 45 * time.Second,
		}
		if test.ServingFlags.HTTPS {
			dialer.TLSClientConfig = test.TLSClientConfig(context.Background(), t.Logf, clients)
			dialer.TLSClientConfig.ServerName = domain // Set ServerName for pseudo hostname with TLS.
		}

		c, resp, err := dialer.Dial(u.String(), http.Header{"Host": {domain}})
		if err == nil {
			t.Log("WebSocket connection established.")
			conn = c
			return true, nil
		}
		if resp == nil {
			// We don't have an HTTP response, probably TCP errors.
			t.Log("Connection failed:", err)
			return false, nil
		}

		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			t.Logf("Connection failed: %v. Failed to read HTTP response: %v", err, readErr)
			return false, nil
		}
		t.Logf("HTTP connection failed: %v. Response=%+v. ResponseBody=%q", err, resp, body)
		return false, nil
	})
	return conn, waitErr
}

func ValidateWebSocketConnection(t *testing.T, clients *test.Clients, names test.ResourceNames, timeoutSeconds string) error {
	t.Helper()
	// Establish the websocket connection.
	conn, err := connect(t, clients, names.URL.Hostname(), timeoutSeconds)
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
