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
	"knative.dev/pkg/websocket"
)

func TestProcessorForwarding(t *testing.T) {
	received := make(chan struct{})
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

	s := httptest.NewServer(httpHandler)
	defer s.Close()

	logger := TestLogger(t)
	conn := websocket.NewDurableSendingConnection("ws"+strings.TrimPrefix(s.URL, "http"), logger)
	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (bool, error) {
		return conn.Status() == nil, nil
	}); err != nil {
		t.Fatal("Timeout waiting f.processors got updated")
	}

	p := bucketProcessor{
		logger: logger,
		bkt:    bucket1,
		conn:   conn,
	}

	p.process(stat1)

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for SVC retry")
	}
}
