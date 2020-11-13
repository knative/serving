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

package handler

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"knative.dev/pkg/websocket"
)

// TimeoutFunc returns the timeout duration to be used by the timeout handler.
type TimeoutFunc func(req *http.Request) time.Duration

// StaticTimeoutFunc returns a TimeoutFunc that always returns the same duration.
func StaticTimeoutFunc(timeout time.Duration) TimeoutFunc {
	return func(req *http.Request) time.Duration {
		return timeout
	}
}

type timeToFirstByteTimeoutHandler struct {
	handler     http.Handler
	timeoutFunc TimeoutFunc
	body        string
}

// NewTimeToFirstByteTimeoutHandler returns a Handler that runs `h` with the
// given time limit from the timeout function in which the first byte of
// the response must be written.
//
// The new Handler calls h.ServeHTTP to handle each request, but if a
// call runs for longer than its time limit, the handler responds with
// a 504 Gateway Timeout error and the given message in its body.
// (If msg is empty, a suitable default message will be sent.)
// After such a timeout, writes by h to its ResponseWriter will return
// ErrHandlerTimeout.
//
// A panic from the underlying handler is propagated as-is to be able to
// make use of custom panic behavior by HTTP handlers. See
// https://golang.org/pkg/net/http/#Handler.
//
// The implementation is largely inspired by http.TimeoutHandler.
func NewTimeToFirstByteTimeoutHandler(h http.Handler, msg string, timeoutFunc TimeoutFunc) http.Handler {
	return &timeToFirstByteTimeoutHandler{
		handler:     h,
		body:        msg,
		timeoutFunc: timeoutFunc,
	}
}

func (h *timeToFirstByteTimeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// done is closed when h.handler.ServeHTTP completes and contains
	// the panic from h.handler.ServeHTTP if h.handler.ServeHTTP panics.
	done := make(chan interface{})
	tw := &timeoutWriter{w: w}
	go func() {
		defer func() {
			defer close(done)
			if p := recover(); p != nil {
				done <- p
			}
		}()
		h.handler.ServeHTTP(tw, r.WithContext(ctx))
	}()

	timeout := time.NewTimer(h.timeoutFunc(r))
	defer timeout.Stop()
	for {
		select {
		case p, ok := <-done:
			if ok {
				panic(p)
			}
			return
		case <-timeout.C:
			if tw.timeoutAndWriteError(h.body) {
				return
			}
		}
	}
}

// timeoutWriter is a wrapper around an http.ResponseWriter. It guards
// writing an error response to whether or not the underlying writer has
// already been written to.
//
// If the underlying writer has not been written to, an error response is
// returned. If it has already been written to, the error is ignored and
// the response is allowed to continue.
type timeoutWriter struct {
	w http.ResponseWriter

	mu        sync.Mutex
	timedOut  bool
	wroteOnce bool
}

var _ http.Flusher = (*timeoutWriter)(nil)
var _ http.ResponseWriter = (*timeoutWriter)(nil)

func (tw *timeoutWriter) Flush() {
	// The inner handler of timeoutHandler can call Flush at any time including after
	// timeoutHandler.ServeHTTP has returned. Forwarding this call to the inner
	// http.ResponseWriter would lead to a panic in HTTP2. See http2/server.go line 2556.
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}

	tw.w.(http.Flusher).Flush()
}

// Hijack calls Hijack() on the wrapped http.ResponseWriter if it implements
// http.Hijacker interface, which is required for net/http/httputil/reverseproxy
// to handle connection upgrade/switching protocol.  Otherwise returns an error.
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return websocket.HijackIfPossible(tw.w)
}

func (tw *timeoutWriter) Header() http.Header { return tw.w.Header() }

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}

	tw.wroteOnce = true
	return tw.w.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.wroteOnce = true
	tw.w.WriteHeader(code)
}

// timeoutAndError writes an error to the response write if
// nothing has been written on the writer before. Returns whether
// an error was written or not.
//
// If this writes an error, all subsequent calls to Write will
// result in http.ErrHandlerTimeout.
func (tw *timeoutWriter) timeoutAndWriteError(msg string) bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if !tw.wroteOnce {
		tw.w.WriteHeader(http.StatusGatewayTimeout)
		io.WriteString(tw.w, msg)

		tw.timedOut = true
		return true
	}

	return false
}
