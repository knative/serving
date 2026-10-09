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
	"cmp"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// HijackTracker is used to track Websocket Connections
// Go net/http by default will not manage connections that
// are hijacked. Thus http.Server::Shutdown will not wait
// for those connections to finish.
//
// What this handler does is track inflight requests
// using a counter and drain will loop and poll until
// all the requests are finished.
type HijackTracker struct {
	Handler      http.Handler
	PollInterval time.Duration

	inflight atomic.Int64
}

type hijackTrackerResponseWriter struct {
	http.ResponseWriter
	tracker *HijackTracker
}

type trackedConn struct {
	net.Conn
	tracker *HijackTracker
	closed  atomic.Bool
}

// Drain should be called after http.Server:Shutdown returns
func (s *HijackTracker) Drain(ctx context.Context) error {
	pollInterval := cmp.Or(s.PollInterval, time.Second)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if s.inflight.Load() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *HijackTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.inflight.Add(1)
	defer s.inflight.Add(-1)

	s.Handler.ServeHTTP(&hijackTrackerResponseWriter{
		ResponseWriter: w,
		tracker:        s,
	}, r)
}

func (w *hijackTrackerResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *hijackTrackerResponseWriter) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
}

func (w *hijackTrackerResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("wrapped writer of type %T can't be hijacked", w.ResponseWriter)
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}

	w.tracker.inflight.Add(1)
	return &trackedConn{
		Conn:    conn,
		tracker: w.tracker,
	}, rw, nil
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	if c.closed.CompareAndSwap(false, true) {
		c.tracker.inflight.Add(-1)
	}
	return err
}
