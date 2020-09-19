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
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTimeoutWriterAllowsForAdditionalWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder}
	handler.WriteHeader(http.StatusOK)
	handler.timeoutAndWriteError("error")
	if _, err := io.WriteString(handler, "test"); err != nil {
		t.Fatalf("handler.Write() = %v, want no error", err)
	}

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Errorf("recorder.Status = %d, want %d", got, want)
	}
	if got, want := recorder.Body.String(), "test"; got != want {
		t.Errorf("recorder.Body = %s, want %s", got, want)
	}
}

func TestTimeoutWriterDoesntFlushAfterTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder}
	handler.timeoutAndWriteError("error")
	handler.Flush()

	if got, want := recorder.Flushed, false; got != want {
		t.Errorf("recorder.Flushed = %t, want %t", got, want)
	}
}

func TestTimeoutWriterFlushesAfterTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder}
	handler.Flush()

	if got, want := recorder.Flushed, true; got != want {
		t.Errorf("recorder.Flushed = %t, want %t", got, want)
	}
}

func TestTimeoutWriterErrorsWriteAfterTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder}
	handler.timeoutAndWriteError("error")
	if _, err := handler.Write([]byte("hello")); err != http.ErrHandlerTimeout {
		t.Errorf("ErrHandlerTimeout got %v, want: %s", err, http.ErrHandlerTimeout)
	}
}

func TestTimeToFirstByteTimeoutHandler(t *testing.T) {
	const (
		immediateTimeout = 0 * time.Millisecond
		longTimeout      = 1 * time.Minute // Super long, not supposed to hit this.
	)

	tests := []struct {
		name           string
		timeoutFunc    TimeoutFunc
		handler        func(mux *sync.Mutex, writeErrors chan error) http.Handler
		timeoutMessage string
		wantStatus     int
		wantBody       string
		wantWriteError bool
		wantPanic      bool
	}{{
		name:        "all good",
		timeoutFunc: StaticTimeoutFunc(longTimeout),
		handler: func(*sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:        "custom timeout message",
		timeoutFunc: StaticTimeoutFunc(immediateTimeout),
		handler: func(mux *sync.Mutex, writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux.Lock()
				defer mux.Unlock()
				_, werr := w.Write([]byte("hi"))
				writeErrors <- werr
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusGatewayTimeout,
		wantBody:       "request timeout",
		wantWriteError: true,
	}, {
		name:        "propagate panic",
		timeoutFunc: StaticTimeoutFunc(longTimeout),
		handler: func(*sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			})
		},
		wantStatus: http.StatusGatewayTimeout,
		wantBody:   "request timeout",
		wantPanic:  true,
	}, {
		name:        "timeout before panic",
		timeoutFunc: StaticTimeoutFunc(immediateTimeout),
		handler: func(*sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(1 * time.Second)
				panic(http.ErrAbortHandler)
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusGatewayTimeout,
		wantBody:       "request timeout",
		wantPanic:      false,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			var reqMux sync.Mutex
			writeErrors := make(chan error, 1)
			rr := httptest.NewRecorder()
			handler := NewTimeToFirstByteTimeoutHandler(test.handler(&reqMux, writeErrors), test.timeoutMessage, test.timeoutFunc)

			defer func() {
				if test.wantPanic {
					if recovered := recover(); recovered != http.ErrAbortHandler {
						t.Errorf("Recover = %v, want: %v", recovered, http.ErrAbortHandler)
					}
				}
			}()

			reqMux.Lock() // Will cause an inner 'Lock' to block. ServeHTTP will exit early if the call times out.
			handler.ServeHTTP(rr, req)
			reqMux.Unlock() // Allows the inner 'Lock' to go through to complete potential writes.

			if status := rr.Code; status != test.wantStatus {
				t.Errorf("Handler returned wrong status code: got %v want %v", status, test.wantStatus)
			}

			if rr.Body.String() != test.wantBody {
				t.Errorf("Handler returned unexpected body: got %q want %q", rr.Body.String(), test.wantBody)
			}

			if test.wantWriteError {
				if err := <-writeErrors; err != http.ErrHandlerTimeout {
					t.Errorf("Expected a timeout error, got %v", err)
				}
			}
		})
	}
}
