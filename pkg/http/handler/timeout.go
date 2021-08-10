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

type timeoutHandler struct {
	handler          http.Handler
	firstByteTimeout time.Duration
	idleTimeout      time.Duration
	body             string
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
func NewTimeoutHandler(h http.Handler, msg string, firstByteTimeout time.Duration, idleTimeout time.Duration) http.Handler {
	return &timeoutHandler{
		handler:          h,
		body:             msg,
		firstByteTimeout: firstByteTimeout,
		idleTimeout:      idleTimeout,
	}
}

func (h *timeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	firstByteTimeout := getTimer(h.firstByteTimeout)
	var firstByteTimeoutDrained bool

	var idleTimeout *optionalTimer
	if h.idleTimeout < 0 {
		idleTimeout = newOptionalTimer(nil)
	} else {
		idleTimeout = newOptionalTimer(getTimer(h.idleTimeout))
	}

	defer func() {
		putTimer(firstByteTimeout, firstByteTimeoutDrained)
		putOptionalTimer(idleTimeout)
	}()

	for {
		select {
		case p, ok := <-done:
			if ok {
				panic(p)
			}
			return
		case <-firstByteTimeout.C:
			firstByteTimeoutDrained = true
			if tw.tryFirstByteTimeoutAndWriteError(h.body) {
				return
			}
		case <-idleTimeout.C():
			timedOut, timeToNextTimeout := tw.tryIdleTimeoutAndWriteError(time.Now(), h.idleTimeout, h.body)
			if timedOut {
				idleTimeout.SetDrained(true)
				return
			}
			idleTimeout.Reset(timeToNextTimeout)
		}
	}
}

// optionalTimer provides a wrapper for time.Timer. When given a not-nil Timer pointer, it
// passes operations to the Timer. When given a nil Timer pointer, it fakes the behavior of
// a timer with a no-op channel and no-op operations
type optionalTimer struct {
	timer    *time.Timer
	drained  bool
	disabled bool
}

func newOptionalTimer(timer *time.Timer) *optionalTimer {
	return &optionalTimer{
		disabled: timer == nil,
		drained:  timer == nil,
		timer:    timer,
	}
}

func (ot *optionalTimer) C() <-chan time.Time {
	if ot.disabled {
		return make(<-chan time.Time)
	}
	return ot.timer.C
}

func (ot *optionalTimer) Timer() (exists bool, timer *time.Timer) {
	if ot.disabled {
		return false, nil
	}
	return true, ot.timer
}

func (ot *optionalTimer) Reset(d time.Duration) bool {
	if ot.disabled {
		return false
	}
	return ot.timer.Reset(d)
}

func (ot *optionalTimer) Drained() bool {
	return ot.drained
}

func (ot *optionalTimer) SetDrained(drained bool) {
	ot.drained = drained
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

	mu            sync.Mutex
	timedOut      bool
	lastWriteTime time.Time
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

	tw.lastWriteTime = time.Now()
	return tw.w.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.lastWriteTime = time.Now()
	tw.w.WriteHeader(code)
}

// tryFirstByteTimeoutAndWriteError writes an error to the responsewriter if
// nothing has been written to the writer before. Returns whether
// an error was written or not.
//
// If this writes an error, all subsequent calls to Write will
// result in http.ErrHandlerTimeout.
func (tw *timeoutWriter) tryFirstByteTimeoutAndWriteError(msg string) bool {
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

func getTimer(timeout time.Duration) *time.Timer {
	if v := timerPool.Get(); v != nil {
		t := v.(*time.Timer)
		t.Reset(timeout)
		return t
	}
	return time.NewTimer(timeout)
}

func putTimer(t *time.Timer, alreadyDrained bool) {
	if !t.Stop() && !alreadyDrained {
		// Stop told us that we didn't *actually* stop the timer, so it expired. We've
		// also not drained the channel yet, so the expiration raced the inner handler
		// finishing, so we know we *have* to drain here.
		<-t.C
	}
	timerPool.Put(t)
}

func putOptionalTimer(t *optionalTimer) {
	if exists, timer := t.Timer(); exists {
		putTimer(timer, t.Drained())
	}
}
