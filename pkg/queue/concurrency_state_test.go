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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/atomic"
	"go.uber.org/zap"
	pkglogging "knative.dev/pkg/logging"
	ltesting "knative.dev/pkg/logging/testing"

	netheader "knative.dev/networking/pkg/http/header"
	netstats "knative.dev/networking/pkg/http/stats"
)

func TestConcurrencyStateHandler(t *testing.T) {
	paused := atomic.NewInt64(0)
	resumed := atomic.NewInt64(0)

	handler := func(w http.ResponseWriter, r *http.Request) {}
	logger := ltesting.TestLogger(t)
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), func(*zap.SugaredLogger) { paused.Inc() }, func(*zap.SugaredLogger) { resumed.Inc() })

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
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), func(*zap.SugaredLogger) { paused.Inc() }, func(*zap.SugaredLogger) { resumed.Inc() })

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
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), func(*zap.SugaredLogger) { paused.Inc() }, func(*zap.SugaredLogger) { resumed.Inc() })

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

func TestConcurrencyStateTokenRefresh(t *testing.T) {
	var token string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tk := r.Header.Get("Token")
		if tk != token {
			t.Errorf("incorrect token header, expected %s, got %s", token, tk)
		}
	}))
	tokenPath := filepath.Join(t.TempDir(), "secret")
	token = "0123456789"
	if err := os.WriteFile(tokenPath, []byte(token), 0700); err != nil {
		t.Fatal(err)
	}

	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	if err := c.Request("pause"); err != nil {
		t.Errorf("initial token check returned an error: %s", err)
	}

	token = "abcdefghijklmnop"
	if err := os.WriteFile(tokenPath, []byte(token), 0700); err != nil {
		t.Fatal(err)
	}
	c.RefreshToken()
	if err := c.Request("pause"); err != nil {
		t.Errorf("updated token check returned an error: %s", err)
	}
}

func TestConcurrencyStateEndpoint(t *testing.T) {
	hostIP := "123.4.56.789"
	os.Setenv("HOST_IP", hostIP)

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}

	// no substitution
	endpoint := "http://test:1234"
	c := NewConcurrencyEndpoint(endpoint, tokenPath)
	if c.endpoint != endpoint {
		t.Errorf("expected %s, got %s", endpoint, c.Endpoint())
	}

	// hostIP substitution
	endpoint = "http://$HOST_IP:1234"
	subEndpoint := "http://" + hostIP + ":1234"
	c = NewConcurrencyEndpoint(endpoint, tokenPath)
	if c.endpoint != subEndpoint {
		t.Errorf("expected %s, got %s", subEndpoint, c.endpoint)
	}

	// hostIP and no port
	endpoint = "http://$HOST_IP"
	c = NewConcurrencyEndpoint(endpoint, tokenPath)
	if c.endpoint != "http://"+hostIP {
		t.Errorf("expected %s, got %s", endpoint, c.Endpoint())
	}

	// non-hostIP not substituted
	endpoint = "http://$SERVING_NAMESPACE:1234"
	c = NewConcurrencyEndpoint(endpoint, tokenPath)
	if c.endpoint != endpoint {
		t.Errorf("expected %s, got %s", endpoint, c.endpoint)
	}
}

func TestConcurrencyStatePauseHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Token")
		if token != "0123456789" {
			t.Errorf("incorrect token header, expected '0123456789', got %s", token)
		}
	}))

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	if err := c.Request("pause"); err != nil {
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

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	if err := c.Request("pause"); err != nil {
		t.Errorf("request test returned an error: %s", err)
	}
}

func TestConcurrencyStateBadResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	if err := c.Request("pause"); err == nil {
		t.Errorf(`Request("pause") function did not return an error`)
	}
	if err := c.Request("resume"); err == nil {
		t.Errorf(`Request("resume") function did not return an error`)
	}
}

func TestConcurrencyStateResumeHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Token")
		if token != "0123456789" {
			t.Errorf("incorrect token header, expected '0123456789', got %s", token)
		}
	}))

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	if err := c.Request("resume"); err != nil {
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

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	if err := c.Request("resume"); err != nil {
		t.Errorf("request test returned an error: %s", err)
	}
}

func TestConcurrencyStateErrorRetryOperation(t *testing.T) {
	reqCnt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqCnt >= 2 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		reqCnt++
	}))
	defer ts.Close()

	tokenPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tokenPath, []byte("0123456789"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewConcurrencyEndpoint(ts.URL, tokenPath)
	handler := func(w http.ResponseWriter, r *http.Request) {}
	logger := ltesting.TestLogger(t)
	h := ConcurrencyStateHandler(logger, http.HandlerFunc(handler), c.Pause, c.Resume)

	timeNow := time.Now()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://target", nil))
	timeAfter := time.Now()
	// why reqCnt is 4: when calling h.ServeHTTP, the server function will call resume, then call pause.
	// it will call resume 3 times to make retry successful because of condition (reqCnt >= 2),  then call
	// pause,  so it's 4 times in together.
	// why time cost can't be less than 400ms: when the first resume failed, it will retry 2 times again, so the time cost
	// is time interval multiplied by 2.
	if timeAfter.Sub(timeNow) < (time.Millisecond*400) || reqCnt != 4 {
		t.Errorf("fail to retry correct times")
	}
}

func BenchmarkConcurrencyStateProxyHandler(b *testing.B) {
	logger, _ := pkglogging.NewLogger("", "error")
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	stats := netstats.NewRequestStats(time.Now())

	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(netheader.OriginalHostKey, wantHost)

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

		pause := func(*zap.SugaredLogger) {}
		resume := func(*zap.SugaredLogger) {}

		h := ConcurrencyStateHandler(logger, ProxyHandler(tc.breaker, stats, true /*tracingEnabled*/, baseHandler), pause, resume)
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
