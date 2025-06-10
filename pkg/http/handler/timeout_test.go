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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"k8s.io/utils/clock"
	clocktest "k8s.io/utils/clock/testing"
)

func TestTimeoutWriterAllowsForAdditionalWritesBeforeTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	clock := clock.RealClock{}
	handler := &timeoutWriter{w: recorder, clock: clock}

	// Write header and some data before any timeout
	handler.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(handler, "test"); err != nil {
		t.Fatalf("handler.Write() = %v, want no error", err)
	}

	// Verify writes succeeded
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Errorf("recorder.Status = %d, want %d", got, want)
	}
	if got, want := recorder.Body.String(), "test"; got != want {
		t.Errorf("recorder.Body = %s, want %s", got, want)
	}

	// Now verify the try methods don't write errors when response has already started
	// tryResponseStartTimeoutAndWriteError should not write error since response already started
	if handler.tryResponseStartTimeoutAndWriteError("error") {
		t.Error("tryResponseStartTimeoutAndWriteError should return false when response already started")
	}

	// tryIdleTimeoutAndWriteError should not timeout immediately after a write
	timedOut, _ := handler.tryIdleTimeoutAndWriteError(clock.Now(), 10*time.Second, "error")
	if timedOut {
		t.Error("tryIdleTimeoutAndWriteError should not timeout immediately after a write")
	}

	// Verify body hasn't changed
	if got, want := recorder.Body.String(), "test"; got != want {
		t.Errorf("recorder.Body = %s, want %s", got, want)
	}
}

func TestTimeoutWriterDoesntFlushAfterTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder, clock: clock.RealClock{}}
	handler.timeoutAndWriteError("error")
	handler.Flush()

	if got, want := recorder.Flushed, false; got != want {
		t.Errorf("recorder.Flushed = %t, want %t", got, want)
	}
}

func TestTimeoutWriterFlushesBeforeTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder, clock: clock.RealClock{}}
	handler.Flush()

	if got, want := recorder.Flushed, true; got != want {
		t.Errorf("recorder.Flushed = %t, want %t", got, want)
	}
}

func TestTimeoutWriterErrorsWriteAfterTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &timeoutWriter{w: recorder, clock: clock.RealClock{}}
	handler.timeoutAndWriteError("error")
	if _, err := handler.Write([]byte("hello")); !errors.Is(err, http.ErrHandlerTimeout) {
		t.Errorf("ErrHandlerTimeout got %v, want: %s", err, http.ErrHandlerTimeout)
	}
}

func TestTryTimeoutAndWriteErrorBehavior(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*timeoutWriter)
		wantWritten    bool
		wantStatusCode int
		wantBody       string
	}{
		{
			name: "writes error when nothing written before and not timed out",
			setup: func(tw *timeoutWriter) {
				// No setup needed - fresh state
			},
			wantWritten:    true,
			wantStatusCode: http.StatusGatewayTimeout,
			wantBody:       "timeout error",
		},
		{
			name: "writes error when response already started but not timed out",
			setup: func(tw *timeoutWriter) {
				// Write something first
				tw.WriteHeader(http.StatusOK)
				tw.Write([]byte("partial response"))
			},
			wantWritten:    true,
			wantStatusCode: http.StatusOK, // Already set
			wantBody:       "partial responsetimeout error",
		},
		{
			name: "does not write error when already timed out",
			setup: func(tw *timeoutWriter) {
				// Simulate a previous timeout
				tw.timedOut = true
			},
			wantWritten:    false,
			wantStatusCode: http.StatusOK, // Default
			wantBody:       "",
		},
		{
			name: "does not write error when already timed out even with prior writes",
			setup: func(tw *timeoutWriter) {
				// Write something first
				tw.WriteHeader(http.StatusAccepted)
				tw.Write([]byte("some data"))
				// Then mark as timed out
				tw.timedOut = true
			},
			wantWritten:    false,
			wantStatusCode: http.StatusAccepted,
			wantBody:       "some data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tw := &timeoutWriter{w: recorder, clock: clock.RealClock{}}

			tt.setup(tw)

			written := tw.tryTimeoutAndWriteError("timeout error")

			if written != tt.wantWritten {
				t.Errorf("tryTimeoutAndWriteError() returned %v, want %v", written, tt.wantWritten)
			}

			if recorder.Code != tt.wantStatusCode {
				t.Errorf("Status code = %d, want %d", recorder.Code, tt.wantStatusCode)
			}

			if recorder.Body.String() != tt.wantBody {
				t.Errorf("Body = %q, want %q", recorder.Body.String(), tt.wantBody)
			}

			// Verify timedOut flag is set correctly
			if tt.wantWritten && !tw.timedOut {
				t.Error("timedOut flag should be true after writing timeout error")
			}
		})
	}
}

type timeoutHandlerTestScenario struct {
	name                 string
	timeout              time.Duration
	responseStartTimeout time.Duration
	idleTimeout          time.Duration
	handler              func(clock *clocktest.FakeClock, mux *sync.Mutex, writeErrors chan error) http.Handler
	timeoutMessage       string
	wantStatus           int
	wantBody             string
	wantWriteError       bool
	wantPanic            bool
}

// This has to be global as the timer cache would otherwise return timers from another clock.
var fakeClock = clocktest.NewFakeClock(time.Time{})

// clearTimerPool drains the global timer pool to ensure tests start with a clean state.
// This is necessary because the timer pool caches timers, and when using a fake clock,
// timers from previous tests can be in unexpected states.
func clearTimerPool() {
	// Drain all timers from the pool
	for {
		if v := timerPool.Get(); v == nil {
			break
		}
	}
}

func testTimeoutScenario(t *testing.T, scenarios []timeoutHandlerTestScenario) {
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			// Clear the timer pool to ensure we start with a clean state
			clearTimerPool()

			var reqMux sync.Mutex
			writeErrors := make(chan error, 1)
			rr := httptest.NewRecorder()

			handler := &timeoutHandler{
				handler:     scenario.handler(fakeClock, &reqMux, writeErrors),
				body:        scenario.timeoutMessage,
				timeoutFunc: StaticTimeoutFunc(scenario.timeout, scenario.responseStartTimeout, scenario.idleTimeout),
				clock:       fakeClock,
			}

			defer func() {
				if scenario.wantPanic {
					if recovered := recover(); recovered != http.ErrAbortHandler { //nolint // False positive for errors.Is check
						t.Errorf("Recover = %v, want: %v", recovered, http.ErrAbortHandler)
					}
				}
			}()

			reqMux.Lock() // Will cause an inner 'Lock' to block. ServeHTTP will exit early if the call times out.
			handler.ServeHTTP(rr, req)
			reqMux.Unlock() // Allows the inner 'Lock' to go through to complete potential writes.

			if status := rr.Code; status != scenario.wantStatus {
				t.Errorf("Handler returned wrong status code: got %v want %v", status, scenario.wantStatus)
			}

			if rr.Body.String() != scenario.wantBody {
				t.Errorf("Handler returned unexpected body: got %q want %q", rr.Body.String(), scenario.wantBody)
			}

			if scenario.wantWriteError {
				if err := <-writeErrors; !errors.Is(err, http.ErrHandlerTimeout) {
					t.Error("Expected a timeout error, got", err)
				}
			}
		})
	}
}

func TestTimeToResponseStartTimeoutHandler(t *testing.T) {
	const (
		immediateTimeout = 1 * time.Millisecond
		longTimeout      = 1 * time.Minute // Super long, not supposed to hit this.
		noIdleTimeout    = 0 * time.Millisecond
	)

	scenarios := []timeoutHandlerTestScenario{{
		name:                 "all good",
		responseStartTimeout: longTimeout,
		idleTimeout:          noIdleTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:                 "custom timeout message",
		responseStartTimeout: immediateTimeout,
		idleTimeout:          noIdleTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Step(immediateTimeout)
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
		name:                 "propagate panic",
		responseStartTimeout: longTimeout,
		idleTimeout:          noIdleTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			})
		},
		wantStatus: http.StatusGatewayTimeout,
		wantBody:   "request timeout",
		wantPanic:  true,
	}, {
		name:                 "timeout before panic",
		responseStartTimeout: immediateTimeout,
		idleTimeout:          noIdleTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, _ chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Step(immediateTimeout)
				mux.Lock()
				defer mux.Unlock()
				panic(http.ErrAbortHandler)
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusGatewayTimeout,
		wantBody:       "request timeout",
		wantPanic:      false,
	}}
	testTimeoutScenario(t, scenarios)
}

func TestResponseStartTimeoutAfterResponseStarted(t *testing.T) {
	// This test verifies that when a response starts before the responseStartTimeout
	// but the timeout still fires, the request completes successfully without hanging.
	// This reproduces the scenario from:
	// https://github.com/knative/serving/issues/15352#issuecomment-2846004329
	//
	// The specific race condition is:
	// 1. Handler writes first byte (response starts)
	// 2. responseStartTimeout timer fires
	// 3. tryResponseStartTimeoutAndWriteError returns false (no error written)
	// 4. Without setting responseStartTimeoutDrained=true, putTimer() hangs trying
	//    to drain the already-drained timer channel

	// Use a channel to control timing precisely
	startWriting := make(chan struct{})
	finishWriting := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait for signal to start writing
		<-startWriting
		w.Write([]byte("response started"))

		// Wait for signal to finish
		<-finishWriting
		w.Write([]byte(" and finished"))
	})

	// Create timeout handler with a responseStartTimeout
	timeoutHandler := NewTimeoutHandler(handler, "timeout",
		StaticTimeoutFunc(30*time.Second, 50*time.Millisecond, 0))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	// Track when ServeHTTP completes
	serveHTTPComplete := make(chan struct{})
	go func() {
		defer close(serveHTTPComplete)
		timeoutHandler.ServeHTTP(rr, req)
	}()

	// Let handler start writing immediately
	close(startWriting)

	// Wait a bit to ensure the response has started and the timer is running
	time.Sleep(10 * time.Millisecond)

	// Now wait for the responseStartTimeout to fire (50ms total)
	time.Sleep(50 * time.Millisecond)

	// Let the handler finish
	close(finishWriting)

	// Wait for ServeHTTP to complete
	// Without the fix (responseStartTimeoutDrained = true), this would hang
	// because putTimer would try to drain an already-drained timer channel
	select {
	case <-serveHTTPComplete:
		// Success - ServeHTTP completed without hanging
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not complete within 2 seconds - likely hanging in putTimer")
	}

	// Verify the response was successful
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	expectedBody := "response started and finished"
	if body := rr.Body.String(); body != expectedBody {
		t.Errorf("Handler returned unexpected body: got %q want %q", body, expectedBody)
	}
}

func TestIdleTimeoutHandler(t *testing.T) {
	const (
		noIdleTimeout            = 0 * time.Millisecond
		immediateIdleTimeout     = 1 * time.Millisecond // 0 would disable the timeout
		shortIdleTimeout         = 100 * time.Millisecond
		longIdleTimeout          = 1 * time.Minute // Super long, not supposed to hit this.
		longResponseStartTimeout = 1 * time.Minute // Super long, not supposed to hit this.
		longTimeout              = 1 * time.Minute // Super long, not supposed to hit this.
	)

	scenarios := []timeoutHandlerTestScenario{{
		name:                 "all good",
		timeout:              longTimeout,
		responseStartTimeout: longResponseStartTimeout,
		idleTimeout:          longIdleTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:                 "custom timeout message",
		timeout:              longTimeout,
		idleTimeout:          immediateIdleTimeout,
		responseStartTimeout: longResponseStartTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Step(immediateIdleTimeout)
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
		name:                 "propagate panic",
		timeout:              longTimeout,
		idleTimeout:          longIdleTimeout,
		responseStartTimeout: longResponseStartTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			})
		},
		wantStatus: http.StatusGatewayTimeout,
		wantBody:   "request timeout",
		wantPanic:  true,
	}, {
		name:                 "timeout before panic",
		timeout:              longTimeout,
		idleTimeout:          immediateIdleTimeout,
		responseStartTimeout: longResponseStartTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, _ chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Step(immediateIdleTimeout)
				mux.Lock()
				defer mux.Unlock()
				panic(http.ErrAbortHandler)
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusGatewayTimeout,
		wantBody:       "request timeout",
		wantPanic:      false,
	}, {
		name:                 "successful writes prevent timeout",
		timeout:              longTimeout,
		idleTimeout:          shortIdleTimeout,
		responseStartTimeout: longResponseStartTimeout,
		handler: func(c *clocktest.FakeClock, _ *sync.Mutex, _ chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("foo"))
				c.Step(shortIdleTimeout - 1*time.Millisecond)
				w.Write([]byte("bar"))
				c.Step(shortIdleTimeout - 1*time.Millisecond)
				w.Write([]byte("baz"))
				c.Step(shortIdleTimeout - 1*time.Millisecond)
				w.Write([]byte("test"))
				c.Step(shortIdleTimeout - 1*time.Millisecond)
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "foobarbaztest",
		wantPanic:  false,
	}, {
		name:                 "can still timeout after a successful write",
		timeout:              longTimeout,
		idleTimeout:          shortIdleTimeout,
		responseStartTimeout: longResponseStartTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, _ chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("foo"))
				c.Step(shortIdleTimeout + 1*time.Millisecond)
				mux.Lock()
				defer mux.Unlock()
				panic(http.ErrAbortHandler)
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusOK,
		wantBody:       "foorequest timeout",
		wantPanic:      false,
	}, {
		name:                 "no idle timeout",
		timeout:              longTimeout,
		idleTimeout:          noIdleTimeout,
		responseStartTimeout: longResponseStartTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			})
		},
		wantStatus: http.StatusGatewayTimeout,
		wantBody:   "request timeout",
		wantPanic:  true,
	}}

	// Max duration is 0, thus disabled
	testTimeoutScenario(t, scenarios)

	// If max duration is set high enough it should not affect any idle timeout test
	for i := range scenarios {
		scenarios[i].timeout = 5 * longIdleTimeout
	}
	testTimeoutScenario(t, scenarios)
}

func TestTimeoutHandler(t *testing.T) {
	const (
		immediateTimeout = 1 * time.Millisecond
		shortTimeout     = 100 * time.Millisecond
		longTimeout      = 1 * time.Minute
	)

	scenarios := []timeoutHandlerTestScenario{{
		name:                 "all good",
		responseStartTimeout: shortTimeout,
		idleTimeout:          shortTimeout,
		timeout:              longTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:                 "max duration timeout with writes",
		responseStartTimeout: shortTimeout,
		idleTimeout:          shortTimeout,
		timeout:              longTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, _ chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Step(longTimeout)
				mux.Lock()
				defer mux.Unlock()
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusGatewayTimeout,
	}, {
		name:                 "propagate panic, max duration too short",
		idleTimeout:          longTimeout,
		responseStartTimeout: longTimeout,
		timeout:              immediateTimeout,
		handler: func(*clocktest.FakeClock, *sync.Mutex, chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(http.ErrAbortHandler)
			})
		},
		wantStatus: http.StatusGatewayTimeout,
		wantBody:   "request timeout",
		wantPanic:  true,
	}, {
		name:                 "timeout before panic, max duration too short",
		idleTimeout:          shortTimeout,
		responseStartTimeout: shortTimeout,
		timeout:              immediateTimeout,
		handler: func(c *clocktest.FakeClock, mux *sync.Mutex, _ chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Step(immediateTimeout)
				mux.Lock()
				defer mux.Unlock()
				panic(http.ErrAbortHandler)
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusGatewayTimeout,
		wantBody:       "request timeout",
		wantPanic:      false,
	}}

	testTimeoutScenario(t, scenarios)
}

func BenchmarkTimeoutHandler(b *testing.B) {
	writes := [][]byte{[]byte("this"), []byte("is"), []byte("a"), []byte("test")}
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for _, write := range writes {
			w.Write(write)
		}
	})
	handler := NewTimeoutHandler(baseHandler, "test", StaticTimeoutFunc(10*time.Minute, 10*time.Minute, 10*time.Minute))
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	b.Run("sequential", func(b *testing.B) {
		resp := httptest.NewRecorder()
		for range b.N {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			resp := httptest.NewRecorder()
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}

func StaticTimeoutFunc(timeout time.Duration, requestStart time.Duration, idle time.Duration) TimeoutFunc {
	return func(req *http.Request) (time.Duration, time.Duration, time.Duration) {
		return timeout, requestStart, idle
	}
}
