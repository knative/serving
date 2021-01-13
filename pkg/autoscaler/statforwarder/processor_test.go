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

package statforwarder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"

	. "knative.dev/pkg/logging/testing"
)

func TestProcessorForwardingViaPodIP(t *testing.T) {
	received := make(chan struct{})

	s := testService(t, received)
	defer s.Close()

	logger := TestLogger(t)
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	p := newForwardProcessor(logger, bucket1, testIP1, url, url)
	defer p.shutdown()

	p.process(stat1)

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for receiving stat")
	}
}

func TestProcessorForwardingViaSvc(t *testing.T) {
	received := make(chan struct{})

	s := testService(t, received)
	defer s.Close()

	logger := TestLogger(t)
	p := newForwardProcessor(logger, bucket1, testIP1, "ws://something.not.working", "ws"+strings.TrimPrefix(s.URL, "http"))
	defer p.shutdown()

	p.process(stat1)

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for receiving stat")
	}
}

func TestProcessorForwardingViaSvcRetry(t *testing.T) {
	received := make(chan struct{})

	s := testService(t, received)
	defer s.Close()

	logger := TestLogger(t)
	p := newForwardProcessor(logger, bucket1, testIP1, "ws://something.not.working", "ws://something.not.working")
	defer p.shutdown()

	if p.conn != nil {
		t.Fatal("Unexpected connection")
	}

	// Change to a working URL
	p.addrs = []string{"ws" + strings.TrimPrefix(s.URL, "http")}
	p.process(stat1)

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for receiving stat")
	}

	if p.conn == nil {
		t.Fatal("Expected a connection but got nil")
	}
}

func testService(t *testing.T, received chan struct{}) *httptest.Server {
	var httpHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		var upgrader gorillawebsocket.Upgrader

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal("error upgrading websocket:", err)
		}

		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				// This is probably caused by connection closed by client side.
				return
			}
			received <- struct{}{}
		}
	}

	return httptest.NewServer(httpHandler)
}
