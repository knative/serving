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
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTimeToFirstByteTimeoutHandler(t *testing.T) {
	const (
		failingTimeout = 0 * time.Millisecond
		longTimeout    = 10 * time.Second
	)

	tests := []struct {
		name           string
		timeout        time.Duration
		handler        func(mux *sync.Mutex, writeErrors chan error) http.Handler
		timeoutMessage string
		wantStatus     int
		wantBody       string
		wantWriteError bool
		wantPanic      bool
	}{{
		name:    "all good",
		timeout: longTimeout,
		handler: func(*sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:    "custom timeout message",
		timeout: failingTimeout,
		handler: func(mux *sync.Mutex, writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux.Lock()
				defer mux.Unlock()
				_, werr := w.Write([]byte("hi"))
				writeErrors <- werr
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusServiceUnavailable,
		wantBody:       "request timeout",
		wantWriteError: true,
	}, {
		name:    "propagate panic",
		timeout: longTimeout,
		handler: func(*sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			})
		},
		wantStatus: http.StatusServiceUnavailable,
		wantBody:   "request timeout",
		wantPanic:  true,
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
			handler := TimeToFirstByteTimeoutHandler(test.handler(&reqMux, writeErrors), test.timeout, test.timeoutMessage)

			defer func() {
				if test.wantPanic {
					if recovered := recover(); recovered != http.ErrAbortHandler {
						t.Error("Expected the handler to panic, but it didn't.")
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
				err := <-writeErrors
				if err != http.ErrHandlerTimeout {
					t.Errorf("Expected a timeout error, got %v", err)
				}
			}
		})
	}
}

func TestTimeoutWriterAllowsForAdditionalWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{
		w: recorder,
	}

	handler.WriteHeader(http.StatusOK)
	handler.TimeoutAndWriteError("error")
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
