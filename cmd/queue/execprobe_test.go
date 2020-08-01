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

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/queue/readiness"
)

func TestProbeQueueInvalidPort(t *testing.T) {
	t.Cleanup(func() { os.Unsetenv(queuePortEnvVar) })
	for _, port := range []string{"-1", "0", "66000"} {
		os.Setenv(queuePortEnvVar, port)
		if rv := standaloneProbeMain(1); rv != 1 {
			t.Error("Unexpected return code", rv)
		}
	}
}

func TestProbeQueueConnectionFailure(t *testing.T) {
	if err := probeQueueHealthPath(1, 12345); err == nil {
		t.Error("Expected error, got nil")
	}
}

func TestProbeQueueNotReady(t *testing.T) {
	queueProbed := 0
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		queueProbed++
		w.WriteHeader(http.StatusBadRequest)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	err = probeQueueHealthPath(time.Second, port)

	if err == nil || err.Error() != "probe returned not ready" {
		t.Error("Unexpected not ready error:", err)
	}

	if queueProbed == 0 {
		t.Error("Expected the queue proxy server to be probed")
	}
}

func TestProbeShuttingDown(t *testing.T) {
	queueProbed := 0
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		queueProbed++
		w.WriteHeader(http.StatusGone)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	err = probeQueueHealthPath(time.Second, port)

	if err == nil || err.Error() != "failed to probe: failing probe deliberately for shutdown" {
		t.Error("Unexpected error:", err)
	}

	if queueProbed == 0 {
		t.Error("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueShuttingDownFailsFast(t *testing.T) {
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusGone)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	start := time.Now()
	if err = probeQueueHealthPath(1, port); err == nil {
		t.Error("probeQueueHealthPath did not fail")
	}

	// if fails due to timeout and not cancelation, then it took too long
	if time.Since(start) >= 1*time.Second {
		t.Error("took too long to fail")
	}
}

func TestProbeQueueReady(t *testing.T) {
	queueProbed := 0
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		queueProbed++
		w.WriteHeader(http.StatusOK)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	t.Cleanup(func() { os.Unsetenv(queuePortEnvVar) })
	os.Setenv(queuePortEnvVar, u.Port())

	if rv := standaloneProbeMain(0 /*use default*/); rv != 0 {
		t.Error("Unexpected return value from standaloneProbeMain:", rv)
	}

	if queueProbed == 0 {
		t.Error("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueTimeout(t *testing.T) {
	queueProbed := 0
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		queueProbed++
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	t.Cleanup(func() { os.Unsetenv(queuePortEnvVar) })
	os.Setenv(queuePortEnvVar, u.Port())

	const timeout = time.Second
	if rv := standaloneProbeMain(timeout); rv == 0 {
		t.Error("Unexpected return value from standaloneProbeMain:", rv)
	}

	ts.Close()

	if queueProbed == 0 {
		t.Error("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueDelayedReady(t *testing.T) {
	count := 0
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		count++
		if count < 9 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("Failed to convert port(%s) to int: %v", u.Port(), err)
	}

	if err := probeQueueHealthPath(readiness.PollTimeout, port); err != nil {
		t.Errorf("probeQueueHealthPath(%d) = %s", port, err)
	}
}

func newProbeTestServer(f func(w http.ResponseWriter)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(network.UserAgentKey) == network.QueueProxyUserAgent {
			f(w)
		}
	}))
}
