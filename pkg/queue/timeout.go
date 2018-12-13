/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package queue

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

var defaultTimeoutBody = "<html><head><title>Timeout</title></head><body><h1>Timeout</h1></body></html>"

// TimeToFirstByteTimeoutHandler returns a Handler that runs h with the
// given time limit in which the first byte of the response must be written.
//
// The new Handler calls h.ServeHTTP to handle each request, but if a
// call runs for longer than its time limit, the handler responds with
// a 503 Service Unavailable error and the given message in its body.
// (If msg is empty, a suitable default message will be sent.)
// After such a timeout, writes by h to its ResponseWriter will return
// ErrHandlerTimeout.
//
// A panic from the underlying handler is propagated as-is to be able to
// make use of custom panic behavior by HTTP handlers. See
// https://golang.org/pkg/net/http/#Handler.
//
// The implementation is largely inspired by http.TimeoutHandler.
func TimeToFirstByteTimeoutHandler(h http.Handler, dt time.Duration, msg string) http.Handler {
	return &timeoutHandler{
		handler: h,
		body:    msg,
		dt:      dt,
	}
}

type timeoutHandler struct {
	handler http.Handler
	body    string
	dt      time.Duration
}

func (h *timeoutHandler) errorBody() string {
	if h.body != "" {
		return h.body
	}
	return defaultTimeoutBody
}

func (h *timeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancelCtx := context.WithCancel(r.Context())
	defer cancelCtx()

	done := make(chan struct{})
	tw := &timeoutWriter{w: w}
	panicChan := make(chan interface{}, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				panicChan <- p
			}
		}()
		h.handler.ServeHTTP(tw, r.WithContext(ctx))

		// Closing the channel is not deferred to give the panic recovery
		// precedence and not successfully complete the request by accident.
		close(done)
	}()

	timeout := time.After(h.dt)
	for {
		select {
		case p := <-panicChan:
			panic(p)
		case <-done:
			return
		case <-timeout:
			func() {
				tw.mu.Lock()
				defer tw.mu.Unlock()

				// Cancel the handler only if the writer has not yet
				// been written to.
				if !tw.wroteOnce {
					w.WriteHeader(http.StatusServiceUnavailable)
					io.WriteString(w, h.errorBody())
					tw.timedOut = true
					cancelCtx()
					return
				}
			}()
		}
	}
}

type timeoutWriter struct {
	w http.ResponseWriter

	mu        sync.Mutex
	timedOut  bool
	wroteOnce bool
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
