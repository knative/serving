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
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clocktest "k8s.io/utils/clock/testing"
)

type timeoutTestConn struct {
	net.Conn
	write func([]byte) (int, error)
}

func (c *timeoutTestConn) Write(p []byte) (int, error) { return c.write(p) }

type timeoutHijackWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
	err  error
}

func (w *timeoutHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.err != nil {
		return nil, nil, w.err
	}
	return w.conn, &bufio.ReadWriter{Writer: bufio.NewWriter(w.conn)}, nil
}

func TestTimeoutWriterTracksHijackedWrites(t *testing.T) {
	testCases := []struct {
		name  string
		write func(net.Conn, *bufio.ReadWriter, string) error
	}{{
		name: "connection",
		write: func(conn net.Conn, _ *bufio.ReadWriter, s string) error {
			_, err := io.WriteString(conn, s)
			return err
		},
	}, {
		name: "buffered_writer",
		write: func(_ net.Conn, rw *bufio.ReadWriter, s string) error {
			_, err := io.WriteString(rw, s)
			if err != nil {
				return err
			}
			return rw.Flush()
		},
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clock := clocktest.NewFakeClock(time.Unix(100, 0))
			tw := &timeoutWriter{clock: clock}
			writeErr := errors.New("write failed")
			tw.w = &timeoutHijackWriter{
				ResponseRecorder: httptest.NewRecorder(),
				conn: &timeoutTestConn{
					write: func([]byte) (int, error) {
						if !tw.lastWriteTime.Equal(clock.Now()) {
							t.Fatal("Write attempt must be recorded before network I/O")
						}
						return 0, writeErr
					},
				},
			}
			conn, rw, err := tw.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			if !tw.lastWriteTime.IsZero() {
				t.Fatal("Hijack without writing must not start the response")
			}
			// Even a failed write attempt must update activity before network I/O.
			err = tc.write(conn, rw, "response")
			if !errors.Is(err, writeErr) {
				t.Fatalf("Write error = %v, want %v", err, writeErr)
			}
		})
	}
}

func TestTimeoutWriterDoesNotWriteErrorAfterHijack(t *testing.T) {
	w := httptest.NewRecorder()
	tw := &timeoutWriter{w: w, hijacked: true}

	timedOut := tw.tryTimeoutAndWriteError("timeout")
	if !timedOut {
		t.Fatal("Overall timeout must still apply after hijacking")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want unchanged HTTP 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("Body = %q, want empty", w.Body.String())
	}
}

func TestTimeoutWriterHijackErrors(t *testing.T) {
	hijackErr := errors.New("hijack failed")
	for _, tc := range []struct {
		name     string
		timedOut bool
		err      error
		want     error
	}{{
		name:     "after_timeout",
		timedOut: true,
		want:     http.ErrHandlerTimeout,
	}, {
		name: "underlying_error",
		err:  hijackErr,
		want: hijackErr,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			w := &timeoutHijackWriter{ResponseRecorder: httptest.NewRecorder(), err: tc.err}
			tw := &timeoutWriter{w: w, timedOut: tc.timedOut}
			conn, rw, err := tw.Hijack()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Hijack error = %v, want %v", err, tc.want)
			}
			if conn != nil {
				t.Fatal("Failed hijack must not return a connection")
			}
			if rw != nil {
				t.Fatal("Failed hijack must not return a buffered reader/writer")
			}
			if tw.hijacked {
				t.Fatal("Failed hijack must not mark the writer as hijacked")
			}
		})
	}
}
