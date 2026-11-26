/*
Copyright 2026 The Knative Authors

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

package handler

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap/zaptest"
)

func TestWebSocketResponseStartTimeoutBeforeHandshake(t *testing.T) {
	const timeout = 500 * time.Millisecond
	// Discard timers cached by fake-clock tests.
	timerPool = sync.Pool{}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never complete the handshake; wait for the proxy to cancel.
		<-r.Context().Done()
	}))
	defer backend.Close()
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	handler := NewTimeoutHandler(proxy, "timeout",
		StaticTimeoutFunc(10*timeout, timeout, 0), zaptest.NewLogger(t).Sugar())
	frontend := httptest.NewServer(handler)
	defer frontend.Close()

	dialer := websocket.Dialer{HandshakeTimeout: 5 * timeout}
	address := "ws" + strings.TrimPrefix(frontend.URL, "http")
	start := time.Now()
	conn, response, err := dialer.Dial(address, nil)
	if response != nil {
		defer response.Body.Close()
	}
	if err == nil {
		conn.Close()
		t.Fatal("Incomplete handshake must exceed the response-start timeout")
	}
	if response == nil {
		t.Fatalf("Dial = %v without a response, want HTTP 504", err)
	}
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("Status = %d, want HTTP 504", response.StatusCode)
	}
	if time.Since(start) < timeout/2 {
		t.Fatal("Request failed before the response-start timeout")
	}
}
