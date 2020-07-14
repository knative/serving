/*
Copyright 2018 The Knative Authors

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

package statserver

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/network"

	"k8s.io/apimachinery/pkg/types"
)

var (
	msg1 = metrics.StatMessage{
		Key: types.NamespacedName{Namespace: "test-namespace", Name: "test-revision"},
		Stat: metrics.Stat{
			PodName:                   "activator1",
			AverageConcurrentRequests: 2.1,
			RequestCount:              51,
		},
	}
	msg2 = metrics.StatMessage{
		Key: types.NamespacedName{Namespace: "test-namespace", Name: "test-revision2"},
		Stat: metrics.Stat{
			PodName:                   "activator2",
			AverageConcurrentRequests: 2.2,
			RequestCount:              30,
		},
	}
)

func TestServerLifecycle(t *testing.T) {
	statsCh := make(chan metrics.StatMessage)
	server := newTestServer(statsCh)

	eg := errgroup.Group{}
	eg.Go(server.listenAndServe)

	server.listenAddr()
	server.Shutdown(time.Second)

	if err := eg.Wait(); err != nil {
		t.Error("listenAndServe failed.", err)
	}
}

func TestProbe(t *testing.T) {
	statsCh := make(chan metrics.StatMessage)
	server := newTestServer(statsCh)

	defer server.Shutdown(0)
	go server.listenAndServe()
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s", server.listenAddr(), network.ProbePath), nil)
	if err != nil {
		t.Fatal("Error creating request:", err)
	}
	req.Header.Set("User-Agent", "kube-probe/1.15.i.wish")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("Error roundtripping:", err)
	}
	defer resp.Body.Close()
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("StatusCode: %v, want: %v", got, want)
	}
}

func TestStatsReceived(t *testing.T) {
	statsCh := make(chan metrics.StatMessage)
	server := newTestServer(statsCh)

	defer server.Shutdown(0)
	go server.listenAndServe()

	statSink := dialOK(t, server.listenAddr())

	// gob encoding
	assertReceivedOK(t, msg1, statSink, statsCh, false)
	assertReceivedOK(t, msg2, statSink, statsCh, false)

	// json encoding
	assertReceivedOK(t, msg1, statSink, statsCh, true)
	assertReceivedOK(t, msg2, statSink, statsCh, true)

	closeSink(t, statSink)
}

func TestServerShutdown(t *testing.T) {
	statsCh := make(chan metrics.StatMessage)
	server := newTestServer(statsCh)

	go server.listenAndServe()

	listenAddr := server.listenAddr()
	statSink := dialOK(t, listenAddr)

	assertReceivedOK(t, msg1, statSink, statsCh, false)

	server.Shutdown(time.Second)
	// We own the channel.
	close(statsCh)

	// Send a statistic to the server
	if err := send(statSink, msg2, false); err != nil {
		t.Fatal("Expected send to succeed, got:", err)
	}

	// Check the statistic was not received
	_, ok := <-statsCh
	if ok {
		t.Fatal("Received statistic after shutdown")
	}

	// Check connection has been closed with a close control message with a "service restart" close code
	if _, _, err := statSink.NextReader(); err == nil {
		t.Fatal("Connection not closed")
	} else {
		err, ok := err.(*websocket.CloseError)
		if !ok {
			t.Fatal("CloseError not received")
		}
		if err.Code != 1012 {
			t.Fatalf("CloseError with unexpected close code %d received", err.Code)
		}
	}

	// Check that new connections are refused with some error
	if _, err := dial(listenAddr); err == nil {
		t.Fatal("Connection not refused")
	}

	closeSink(t, statSink)
}

func TestServerDoesNotLeakGoroutines(t *testing.T) {
	statsCh := make(chan metrics.StatMessage)
	server := newTestServer(statsCh)
	defer server.Shutdown(0)

	go server.listenAndServe()

	originalGoroutines := runtime.NumGoroutine()

	listenAddr := server.listenAddr()
	statSink := dialOK(t, listenAddr)

	assertReceivedOK(t, msg1, statSink, statsCh, false)

	closeSink(t, statSink)

	// Check the number of goroutines eventually reduces to the number there were before the connection was created
	for i := 1000; i >= 0; i-- {
		currentGoRoutines := runtime.NumGoroutine()
		if currentGoRoutines <= originalGoroutines {
			break
		}
		time.Sleep(5 * time.Millisecond)
		if i == 0 {
			t.Fatalf("Current number of goroutines %d is not equal to the original number %d", currentGoRoutines, originalGoroutines)
		}
	}
}

func BenchmarkStatServer(b *testing.B) {
	statsCh := make(chan metrics.StatMessage, 100)
	server := newTestServer(statsCh)
	go server.listenAndServe()
	defer server.Shutdown(time.Second)

	statSink, err := dial(server.listenAddr())
	if err != nil {
		b.Fatal("Dial failed:", err)
	}

	// The activator sends a bunch of metrics at once usually. This simulates cases with
	// the respective number of active revisions, sending via the activator.
	for _, size := range []int{1, 2, 5, 10, 20, 50, 100} {
		msgs := make([]metrics.StatMessage, 0, size)
		for i := 0; i < size; i++ {
			msgs = append(msgs, msg1)
		}

		for encoding, jsonEncoding := range map[string]bool{"json": true, "gob": false} {
			b.Run(fmt.Sprintf("%s-encoding-%d-msgs", encoding, len(msgs)), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					for _, msg := range msgs {
						if err := send(statSink, msg, jsonEncoding); err != nil {
							b.Fatal("Expected send to succeed, but got:", err)
						}
					}

					for range msgs {
						<-statsCh
					}
				}
			})
		}
	}
}

func assertReceivedOK(t *testing.T, sm metrics.StatMessage, statSink *websocket.Conn, statsCh <-chan metrics.StatMessage, jsonEncoding bool) {
	if err := send(statSink, sm, jsonEncoding); err != nil {
		t.Fatal("Expected send to succeed, got:", err)
	}

	recv := <-statsCh
	if !cmp.Equal(sm, recv) {
		t.Fatalf("StatMessage mismatch: diff (-got, +want) %s", cmp.Diff(recv, sm))
	}
}

func dialOK(t *testing.T, serverURL string) *websocket.Conn {
	statSink, err := dial(serverURL)
	if err != nil {
		t.Fatal("Dial failed:", err)
	}
	return statSink
}

func dial(serverURL string) (*websocket.Conn, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	u.Scheme = "ws"

	dialer := &websocket.Dialer{
		HandshakeTimeout: time.Second,
	}
	statSink, _, err := dialer.Dial(u.String(), nil)
	return statSink, err
}

func send(statSink *websocket.Conn, sm metrics.StatMessage, jsonEncoding bool) error {
	var b bytes.Buffer

	var enc encoder = gob.NewEncoder(&b)
	messageType := websocket.BinaryMessage

	if jsonEncoding {
		enc = json.NewEncoder(&b)
		messageType = websocket.TextMessage
	}

	if err := enc.Encode(sm); err != nil {
		return fmt.Errorf("failed to encode data from stats channel: %w", err)
	}

	if err := statSink.WriteMessage(messageType, b.Bytes()); err != nil {
		return fmt.Errorf("failed to write to stat sink: %w", err)
	}

	return nil
}

func closeSink(t *testing.T, statSink *websocket.Conn) {
	if err := statSink.Close(); err != nil {
		t.Fatal("Failed to close", err)
	}
}

const testAddress = "127.0.0.1:0"

type testServer struct {
	*Server
	listenAddrCh chan string
}

func newTestServer(statsCh chan<- metrics.StatMessage) *testServer {
	return &testServer{
		Server:       New(testAddress, statsCh, zap.NewNop().Sugar()),
		listenAddrCh: make(chan string, 1),
	}
}

// listenAddr returns the address on which the server is listening. Blocks until listenAndServe is called.
func (s *testServer) listenAddr() string {
	return <-s.listenAddrCh
}

func (s *testServer) listenAndServe() error {
	listener, err := s.listen()
	if err != nil {
		return err
	}
	return s.serve(&testListener{listener, s.listenAddrCh})
}

type testListener struct {
	net.Listener
	listenAddr chan string
}

func (t *testListener) Accept() (net.Conn, error) {
	t.listenAddr <- "http://" + t.Listener.Addr().String()
	return t.Listener.Accept()
}

// encoder is the interface implemented by gob.Encoder and json.Encoder
type encoder interface {
	Encode(interface{}) error
}
