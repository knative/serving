/*
Copyright 2021 The Knative Authors

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

package queue

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/atomic"
	"go.uber.org/zap"
	pkglogging "knative.dev/pkg/logging"
	ltesting "knative.dev/pkg/logging/testing"

	network "knative.dev/networking/pkg"
)

func TestConcurrencyStateHandler(t *testing.T) {
	paused := atomic.NewInt64(0)
	resumed := atomic.NewInt64(0)

	handler := func(w http.ResponseWriter, r *http.Request) {}
	logger := ltesting.TestLogger(t)
	tokenFile := createTempTokenFile(logger)
	defer os.Remove(tokenFile.Name())
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), func(*Token) error { paused.Inc(); return nil }, func(*Token) error { resumed.Inc(); return nil }, tokenFile.Name())

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target", nil))
	if got, want := paused.Load(), int64(1); got != want {
		t.Errorf("Pause was called %d times, want %d times", got, want)
	}

	if got, want := resumed.Load(), int64(1); got != want {
		t.Errorf("Resume was called %d times, want %d times", got, want)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target", nil))
	if got, want := paused.Load(), int64(2); got != want {
		t.Errorf("Pause was called %d times, want %d times", got, want)
	}

	if got, want := resumed.Load(), int64(2); got != want {
		t.Errorf("Resume was called %d times, want %d times", got, want)
	}
}

func TestConcurrencyStateHandlerParallelSubsumed(t *testing.T) {
	paused := atomic.NewInt64(0)
	resumed := atomic.NewInt64(0)

	req1 := make(chan struct{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("req") == "1" {
			req1 <- struct{}{} // to know it's here.
			req1 <- struct{}{} // to make it wait.
		}
	}
	logger := ltesting.TestLogger(t)
	tokenFile := createTempTokenFile(logger)
	defer os.Remove(tokenFile.Name())
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), func(*Token) error { paused.Inc(); return nil }, func(*Token) error { resumed.Inc(); return nil }, tokenFile.Name())

	go func() {
		defer func() { req1 <- struct{}{} }()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target?req=1", nil))
	}()

	<-req1 // Wait for req1 to arrive.

	// Send a second request, which can pass through.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target", nil))

	<-req1 // Allow req1 to pass.
	<-req1 // Wait for req1 to finish.

	if got, want := paused.Load(), int64(1); got != want {
		t.Errorf("Pause was called %d times, want %d times", got, want)
	}

	if got, want := resumed.Load(), int64(1); got != want {
		t.Errorf("Resume was called %d times, want %d times", got, want)
	}
}

func TestConcurrencyStateHandlerParallelOverlapping(t *testing.T) {
	paused := atomic.NewInt64(0)
	resumed := atomic.NewInt64(0)

	req1 := make(chan struct{})
	req2 := make(chan struct{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("req") == "1" {
			req1 <- struct{}{} // to know it's here.
			req1 <- struct{}{} // to make it wait.
		} else {
			req2 <- struct{}{} // to know it's here.
			req2 <- struct{}{} // to make it wait.
		}
	}
	logger := ltesting.TestLogger(t)
	tokenFile := createTempTokenFile(logger)
	defer os.Remove(tokenFile.Name())
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), func(*Token) error { paused.Inc(); return nil }, func(*Token) error { resumed.Inc(); return nil }, tokenFile.Name())

	go func() {
		defer func() { req1 <- struct{}{} }()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target?req=1", nil))
	}()

	<-req1 // Wait for req1 to arrive.

	go func() {
		defer func() { req2 <- struct{}{} }()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target?req=2", nil))
	}()

	<-req2 // Wait for req2 to arrive

	<-req1 // Allow req1 to pass.
	<-req1 // Wait for req1 to finish.

	<-req2 // Allow req2 to pass.
	<-req2 // Wait for req2 to finish.

	if got, want := paused.Load(), int64(1); got != want {
		t.Errorf("Pause was called %d times, want %d times", got, want)
	}

	if got, want := resumed.Load(), int64(1); got != want {
		t.Errorf("Resume was called %d times, want %d times", got, want)
	}
}

func TestConcurrencyStatePauseHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			if k == "Token" {
				if v[0] != "0123456789" {
					t.Errorf("incorrect token header, expected '0123456789', got %s", v)
				}
			}
		}
	}))
	tempToken := createTempToken()
	pause := Pause(ts.URL)
	if err := pause(tempToken); err != nil {
		t.Errorf("pause header check returned an error: %s", err)
	}
}

func TestConcurrencyStatePauseRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		var m struct{ Action string }
		err := json.NewDecoder(r.Body).Decode(&m)
		if err != nil {
			t.Errorf("unable to parse message body: %s", err)
		}
		if m.Action != "pause" {
			t.Errorf("improper message body, expected 'pause' and got: %s", m.Action)
		}
	}))

	pause := Pause(ts.URL)
	tempToken := createTempToken()
	if err := pause(tempToken); err != nil {
		t.Errorf("request test returned an error: %s", err)
	}
}

func TestConcurrencyStatePauseResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	pause := Pause(ts.URL)
	tempToken := createTempToken()
	if err := pause(tempToken); err == nil {
		t.Errorf("pausefunction did not return an error")
	}
}

func TestConcurrencyStateResumeHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			if k == "Token" {
				if v[0] != "0123456789" {
					t.Errorf("incorrect token header, expected '0123456789', got %s", v)
				}
			}
		}
	}))
	tempToken := createTempToken()
	resume := Resume(ts.URL)
	if err := resume(tempToken); err != nil {
		t.Errorf("resume header check returned an error: %s", err)
	}
}

func TestConcurrencyStateResumeRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		var m struct{ Action string }
		err := json.NewDecoder(r.Body).Decode(&m)
		if err != nil {
			t.Errorf("unable to parse message body: %s", err)
		}
		if m.Action != "resume" {
			t.Errorf("improper message body, expected 'resume' and got: %s", m.Action)
		}
	}))

	resume := Resume(ts.URL)
	tempToken := createTempToken()
	if err := resume(tempToken); err != nil {
		t.Errorf("request test returned an error: %s", err)
	}
}

func TestConcurrencyStateResumeResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	resume := Resume(ts.URL)
	tempToken := createTempToken()
	if err := resume(tempToken); err == nil {
		t.Errorf("resume function did not return an error")
	}
}

func BenchmarkConcurrencyStateProxyHandler(b *testing.B) {
	logger, _ := pkglogging.NewLogger("", "error")
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	stats := network.NewRequestStats(time.Now())

	promStatReporter, err := NewPrometheusStatsReporter(
		"ns", "testksvc", "testksvc",
		"pod", reportingPeriod)
	if err != nil {
		b.Fatal("Failed to create stats reporter:", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(network.OriginalHostHeader, wantHost)

	tokenFile := createTempTokenFile(logger)
	defer os.Remove(tokenFile.Name())

	tests := []struct {
		label        string
		breaker      *Breaker
		reportPeriod time.Duration
	}{{
		label:        "breaker-10-no-reports",
		breaker:      NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		reportPeriod: time.Hour,
	}, {
		label:        "breaker-infinite-no-reports",
		breaker:      nil,
		reportPeriod: time.Hour,
	}, {
		label:        "breaker-10-many-reports",
		breaker:      NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		reportPeriod: time.Microsecond,
	}, {
		label:        "breaker-infinite-many-reports",
		breaker:      nil,
		reportPeriod: time.Microsecond,
	}}

	for _, tc := range tests {
		reportTicker := time.NewTicker(tc.reportPeriod)

		go func() {
			for now := range reportTicker.C {
				promStatReporter.Report(stats.Report(now))
			}
		}()
		pause := func(*Token) error {
			return nil
		}
		resume := func(*Token) error {
			return nil
		}

		h := ConcurrencyStateHandler(logger, ProxyHandler(tc.breaker, stats, true /*tracingEnabled*/, baseHandler), pause, resume, tokenFile.Name())
		b.Run("sequential-"+tc.label, func(b *testing.B) {
			resp := httptest.NewRecorder()
			for j := 0; j < b.N; j++ {
				h(resp, req)
			}
		})
		b.Run("parallel-"+tc.label, func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				resp := httptest.NewRecorder()
				for pb.Next() {
					h(resp, req)
				}
			})
		})

		reportTicker.Stop()
	}
}

// createTempTokenFile creates a temporary file with the text "0123456789" for simulating a serviceAccountToken
// Note that it does NOT delete the temp file, this must be called separately, for example:
//
// tempFile := createTempTokenFile(logger)
// defer os.Remove(tempFile.Name())
func createTempTokenFile(logger *zap.SugaredLogger) *os.File {
	tokenFile, err := ioutil.TempFile("", "secret")
	if err != nil {
		logger.Fatal(err)
	}
	if _, err := tokenFile.Write([]byte("0123456789")); err != nil {
		logger.Fatal(err)
	}
	if err := tokenFile.Close(); err != nil {
		logger.Fatal(err)
	}
	return tokenFile
}

func createTempToken() *Token {
	token := Token{token: "0123456789"}
	return &token
}
