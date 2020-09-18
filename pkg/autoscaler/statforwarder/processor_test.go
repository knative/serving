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
	"k8s.io/apimachinery/pkg/util/wait"

	. "knative.dev/pkg/logging/testing"
)

func TestProcessorForwardingViaPodIP(t *testing.T) {
	received := make(chan struct{})

	s := testService(t, received)
	defer s.Close()

	logger := TestLogger(t)
	p := newForwardProcessor(logger, bucket1, testIP1, "ws"+strings.TrimPrefix(s.URL, "http"), "ws"+strings.TrimPrefix(s.URL, "http"))

	// Wait connection via IP to be established.
	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (bool, error) {
		return p.podConn != nil && p.podConn.Status() == nil, nil
	}); err != nil {
		t.Fatal("Timeout waiting for connection established")
	}

	p.process(stat1)

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for receiving stat")
	}

	if got, wanted := p.podAddressable, true; got != wanted {
		t.Errorf("podsAddressable = %v, want = %v", got, wanted)
	}
	if p.svcConn != nil {
		t.Error("Expected connection via SVC to be closed but not")
	}
}

func TestProcessorForwardingViaSvc(t *testing.T) {
	received := make(chan struct{})

	s := testService(t, received)
	defer s.Close()

	logger := TestLogger(t)
	p := newForwardProcessor(logger, bucket1, testIP1, "ws://something.not.working", "ws"+strings.TrimPrefix(s.URL, "http"))

	// Wait connection via SVC to be established.
	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (bool, error) {
		return p.svcConn != nil && p.svcConn.Status() == nil, nil
	}); err != nil {
		t.Fatal("Timeout waiting for connection established")
	}

	p.process(stat1)

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for receiving stat")
	}

	if got, wanted := p.podAddressable, false; got != wanted {
		t.Errorf("podsAddressable = %v, want = %v", got, wanted)
	}
	if p.podConn != nil {
		t.Error("Expected connection via Pod IP to be closed but not")
	}
}

func TestProcessoReconnect(t *testing.T) {
	received := make(chan struct{})

	s := testService(t, received)
	defer s.Close()

	logger := TestLogger(t)
	p := newForwardProcessor(logger, bucket1, testIP1, "ws://something.not.working", "ws"+strings.TrimPrefix(s.URL, "http"))

	// Simulate that we have succseefully sent via Pod IP and closed the connection via SVC.
	p.svcConn.Shutdown()
	p.svcConn = nil

	p.process(stat1)

	if p.svcConn == nil {
		t.Error("Expected connection via SVC to be created but not")
	}
}

func testService(t *testing.T, received chan struct{}) *httptest.Server {
	var httpHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		var upgrader gorillawebsocket.Upgrader

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("error upgrading websocket: %v", err)
		}

		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("error reading message: %v", err)
			}
			received <- struct{}{}
		}
	}

	return httptest.NewServer(httpHandler)
}
