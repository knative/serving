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

	"k8s.io/utils/clock"
	"knative.dev/pkg/websocket"
)

// TimeoutFunc returns the timeout duration to be used by the timeout handler.
type TimeoutFunc func(req *http.Request) (time.Duration, time.Duration, time.Duration)

type timeoutHandler struct {
	handler     http.Handler
	timeoutFunc TimeoutFunc
	body        string
	clock       clock.Clock
}

// NewTimeoutHandler returns a Handler that runs `h` with the
// given timeout in which the first byte of the response must be written,
// and with the given idle timeout
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
func NewTimeoutHandler(h http.Handler, msg string, timeoutFunc TimeoutFunc) http.Handler {
	return &timeoutHandler{
		handler:     h,
		body:        msg,
		timeoutFunc: timeoutFunc,
		clock:       clock.RealClock{},
	}
}

func (h *timeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	revTimeout, revResponseStartTimeout, revIdleTimeout := h.timeoutFunc(r)

	timeout := getTimer(h.clock, revTimeout)
	var timeoutDrained bool
	defer func() {
		putTimer(timeout, timeoutDrained)
	}()

	var idleTimeout clock.Timer
	var idleTimeoutDrained bool
	if revIdleTimeout > 0 {
		idleTimeout = getTimer(h.clock, revIdleTimeout)
		defer func() {
			putTimer(idleTimeout, idleTimeoutDrained)
		}()
	}
	var idleTimeoutCh <-chan time.Time
	if idleTimeout != nil {
		idleTimeoutCh = idleTimeout.C()
	}

	// done is closed when h.handler.ServeHTTP completes and contains
	// the panic from h.handler.ServeHTTP if h.handler.ServeHTTP panics.
	done := make(chan interface{})
	tw := &timeoutWriter{w: w, clock: h.clock}

	var responseStartTimeout clock.Timer
	var responseStartTimeoutDrained bool
	if revResponseStartTimeout > 0 {
		responseStartTimeout = getTimer(h.clock, revResponseStartTimeout)
		defer func() {
			putTimer(responseStartTimeout, responseStartTimeoutDrained)
		}()
	}
	var responseStartTimeoutCh <-chan time.Time
	if responseStartTimeout != nil {
		responseStartTimeoutCh = responseStartTimeout.C()
	}

	go func() {
		defer func() {
			defer close(done)
			if p := recover(); p != nil {
				done <- p
			}
		}()

		h.handler.ServeHTTP(tw, r.WithContext(ctx))
	}()

	for {
		select {
		case p, ok := <-done:
			if ok {
				panic(p)
			}
			return
		case <-timeout.C():
			timeoutDrained = true
			if tw.tryTimeoutAndWriteError(h.body) {
				return
			}
		case now := <-idleTimeoutCh:
			timedOut, timeToNextTimeout := tw.tryIdleTimeoutAndWriteError(now, revIdleTimeout, h.body)
			if timedOut {
				idleTimeoutDrained = true
				return
			}
			idleTimeout.Reset(timeToNextTimeout)
		case <-responseStartTimeoutCh:
			timedOut := tw.tryResponseStartTimeoutAndWriteError(h.body)
			if timedOut {
				responseStartTimeoutDrained = true
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
	w     http.ResponseWriter
	clock clock.PassiveClock

	mu            sync.Mutex
	timedOut      bool
	lastWriteTime time.Time
}

var _ http.Flusher = (*timeoutWriter)(nil)
var _ http.ResponseWriter = (*timeoutWriter)(nil)

// Unwrap returns the underlying writer
func (tw *timeoutWriter) Unwrap() http.ResponseWriter {
	return tw.w
}

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

	tw.lastWriteTime = tw.clock.Now()
	return tw.w.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.lastWriteTime = tw.clock.Now()
	tw.w.WriteHeader(code)
}

// tryTimeoutAndWriteError writes an error to the responsewriter if
// nothing has been written to the writer before. Returns whether
// an error was written or not.
//
// If this writes an error, all subsequent calls to Write will
// result in http.ErrHandlerTimeout.
func (tw *timeoutWriter) tryTimeoutAndWriteError(msg string) bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.lastWriteTime.IsZero() {
		tw.timeoutAndWriteError(msg)
		return true
	}

	return false
}

// tryResponseStartTimeoutAndWriteError writes an error to the responsewriter if
// the response has not started responding before. Returns whether an error was
// written or not.
//
// If this writes an error, all subsequent calls to Write will
// result in http.ErrHandlerTimeout.
func (tw *timeoutWriter) tryResponseStartTimeoutAndWriteError(msg string) bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.lastWriteTime.IsZero() {
		tw.timeoutAndWriteError(msg)
		return true
	}

	return false
}

// tryIdleTimeoutAndWriteError writes an error to the responsewriter if
// nothing has been written to the writer within the idleTimeout period. Returns whether
// an error was written or not and time left to the next timeout
//
// If this writes an error, all subsequent calls to Write will
// result in http.ErrHandlerTimeout.
func (tw *timeoutWriter) tryIdleTimeoutAndWriteError(curTime time.Time, idleTimeout time.Duration, msg string) (timedOut bool, timeToNextTimeout time.Duration) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	timeSinceLastWrite := curTime.Sub(tw.lastWriteTime)
	if timeSinceLastWrite >= idleTimeout {
		tw.timeoutAndWriteError(msg)
		return true, 0
	}

	return false, idleTimeout - timeSinceLastWrite
}

func (tw *timeoutWriter) timeoutAndWriteError(msg string) {
	tw.w.WriteHeader(http.StatusGatewayTimeout)
	io.WriteString(tw.w, msg)

	tw.timedOut = true
}

var timerPool sync.Pool

func getTimer(c clock.Clock, timeout time.Duration) clock.Timer {
	if v := timerPool.Get(); v != nil {
		t := v.(clock.Timer)
		t.Reset(timeout)
		return t
	}
	return c.NewTimer(timeout)
}

func putTimer(t clock.Timer, alreadyDrained bool) {
	if !t.Stop() && !alreadyDrained {
		// Stop told us that we didn't *actually* stop the timer, so it expired. We've
		// also not drained the channel yet, so the expiration raced the inner handler
		// finishing, so we know we *have* to drain here.
		<-t.C()
	}
	timerPool.Put(t)
}
