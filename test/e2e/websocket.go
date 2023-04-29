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
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"

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
func connect(t *testing.T, clients *test.Clients, domain, timeout string) (net.Conn, error) {
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

	var conn net.Conn
	waitErr := wait.PollImmediate(connectRetryInterval, connectTimeout, func() (bool, error) {
		t.Logf("Connecting using websocket: url=%s, host=%s", u.String(), domain)
		dialer := &ws.Dialer{
			Timeout: 45 * time.Second,
			Header:  ws.HandshakeHeaderHTTP(http.Header{"Host": {domain}}),
			//debug TODO: delete later
			OnStatusError: func(status int, reason []byte, resp io.Reader) {
				var b bytes.Buffer
				io.Copy(&b, resp)
				t.Logf("HTTP Status Error: %d %s\nBody: %s", status, reason, b.String())
			},
		}
		if test.ServingFlags.HTTPS {
			dialer.TLSConfig = test.TLSClientConfig(context.Background(), t.Logf, clients)
			dialer.TLSConfig.ServerName = domain // Set ServerName for pseudo hostname with TLS.
		}

		ctx := context.TODO()

		// TODO: delete later
		httpURL := replaceWebSocketSchemeWithHTTP(u.String())
		t.Logf("HTTP URL %s", httpURL)
		req, err := http.NewRequest(http.MethodGet, httpURL, nil)
		if err != nil {
			t.Logf("HTTP New Request error: %s", err)
		}
		proxyURL, err := http.ProxyFromEnvironment(req)
		if err != nil {
			t.Logf("ProxyFromEnvironment error: %s", err)
		}
		t.Logf("proxyURL: %#v", proxyURL)
		httpProxy := os.Getenv("http_proxy")
		t.Logf("http_proxy: %s", httpProxy)
		httpsProxy := os.Getenv("https_proxy")
		t.Logf("https_proxy: %s", httpsProxy)
		noProxy := os.Getenv("no_proxy")
		t.Logf("no_proxy: %s", noProxy)

		c, _, _, err := dialer.Dial(ctx, u.String())
		if err != nil {
			// We don't have an HTTP response, probably TCP errors.
			t.Log("Connection failed:", err)
			return false, nil
		}
		if c != nil {
			t.Log("WebSocket connection established.")
			conn = c
			return true, nil
		}
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
	if err = wsutil.WriteMessage(conn, ws.StateClientSide, ws.OpText, []byte(message)); err != nil {
		return err
	}
	t.Log("Message sent.")

	// Read back the echoed message and compared with sent.
	var messages []wsutil.Message
	messages, err = wsutil.ReadMessage(conn, ws.StateClientSide, messages)
	recv := messages[0].Payload
	if err != nil {
		return err
	} else if strings.HasPrefix(string(recv), message) {
		t.Logf("Received message %q from echo server.", recv)
		return nil
	}
	return fmt.Errorf("expected to receive back the message: %q but received %q", message, string(recv))
}

func replaceWebSocketSchemeWithHTTP(url string) string {
	if strings.HasPrefix(url, "ws://") {
		return "http://" + url[5:]
	} else if strings.HasPrefix(url, "wss://") {
		return "https://" + url[6:]
	}
	return url
}
