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
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type hijackableResponseWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	client, server := net.Pipe()
	w.conn = client
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}

type flushableResponseWriter struct {
	*hijackableResponseWriter
	flushed bool
}

func (w *flushableResponseWriter) Flush() {
	w.flushed = true
}

func TestHijackTrackerNoHijack(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://somehost.com", nil)

	h := &HijackTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	h.ServeHTTP(w, r)

	err := h.Drain(context.Background())
	if err != nil {
		t.Fatal("unexpected error while draining", err)
	}
}

func TestHijackTrackerConnectionHijacked(t *testing.T) {
	w := &hijackableResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "http://somehost.com", nil)

	inHandler := make(chan struct{})
	connClosed := make(chan struct{})
	drainResult := make(chan error, 1)

	h := &HijackTracker{
		PollInterval: 10 * time.Millisecond,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(inHandler)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatalf("Hijack() = %v", err)
			}
			go func() {
				<-connClosed
				conn.Close()
			}()
		}),
	}
	defer func() {
		if w.conn != nil {
			w.conn.Close()
		}
	}()

	go func() {
		h.ServeHTTP(w, r)
	}()

	select {
	case <-inHandler:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("control flow never reached the http handler")
	}

	go func() {
		drainResult <- h.Drain(context.Background())
	}()

	select {
	case <-time.After(250 * time.Millisecond):
	case <-drainResult:
		t.Fatal("drain returned before hijacked connection was closed")
	}

	close(connClosed)

	var err error
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("Drain was not unblocked when the hijacked connection closed")
	case err = <-drainResult:
	}

	if err != nil {
		t.Fatal("unexpected error draining", err)
	}
}

func TestHijackTrackerConnectionHijackedTimeout(t *testing.T) {
	w := &hijackableResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "http://somehost.com", nil)

	inHandler := make(chan struct{})
	drainStarted := make(chan struct{})
	drainResult := make(chan error, 1)

	h := &HijackTracker{
		PollInterval: 10 * time.Millisecond,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(inHandler)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatalf("Hijack() = %v", err)
			}
			_ = conn
		}),
	}
	defer func() {
		if w.conn != nil {
			w.conn.Close()
		}
	}()

	go func() {
		h.ServeHTTP(w, r)
	}()

	go func() {
		<-inHandler
		close(drainStarted)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		drainResult <- h.Drain(ctx)
	}()

	<-drainStarted

	var err error
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("Drain did not timeout")
	case err = <-drainResult:
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unexpected error draining", err)
	}
}

func TestHijackTrackerResponseWriterPreservesOptionalInterfaces(t *testing.T) {
	w := &flushableResponseWriter{
		hijackableResponseWriter: &hijackableResponseWriter{
			ResponseRecorder: httptest.NewRecorder(),
		},
	}

	h := &HijackTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.(http.Flusher).Flush()
			got := w.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
			if got != w.(*hijackTrackerResponseWriter).ResponseWriter {
				t.Fatal("Unwrap() did not return the underlying writer")
			}
		}),
	}

	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://somehost.com", nil))

	if !w.flushed {
		t.Fatal("Flush() was not forwarded to the underlying writer")
	}
}
