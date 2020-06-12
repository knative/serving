package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/network"
)

func TestProbeQueueInvalidPort(t *testing.T) {
	if err := probeQueueHealthPath(1, 0); err == nil {
		t.Error("Expected error, got nil")
	} else if diff := cmp.Diff(err.Error(), "port must be a positive value, got 0"); diff != "" {
		t.Errorf("Unexpected not ready message: %s", diff)
	}
}

func TestProbeQueueConnectionFailure(t *testing.T) {
	if err := probeQueueHealthPath(1, 12345); err == nil {
		t.Error("Expected error, got nil")
	}
}

func TestProbeQueueNotReady(t *testing.T) {
	queueProbed := ptr.Int32(0)
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		atomic.AddInt32(queueProbed, 1)
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

	err = probeQueueHealthPath(1, port)

	if diff := cmp.Diff(err.Error(), "probe returned not ready"); diff != "" {
		t.Errorf("Unexpected not ready message: %s", diff)
	}

	if atomic.LoadInt32(queueProbed) == 0 {
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
	queueProbed := ptr.Int32(0)
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		atomic.AddInt32(queueProbed, 1)
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

	if err = probeQueueHealthPath(1, port); err != nil {
		t.Errorf("probeQueueHealthPath(%d, 1s) = %s", port, err)
	}

	if atomic.LoadInt32(queueProbed) == 0 {
		t.Error("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueTimeout(t *testing.T) {
	queueProbed := ptr.Int32(0)
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		atomic.AddInt32(queueProbed, 1)
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("%s is not a valid URL: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to convert port(%s) to int", u.Port())
	}

	timeout := 1
	if err = probeQueueHealthPath(timeout, port); err == nil {
		t.Errorf("Expected probeQueueHealthPath(%d, %v) to return timeout error", port, timeout)
	}

	ts.Close()

	if atomic.LoadInt32(queueProbed) == 0 {
		t.Error("Expected the queue proxy server to be probed")
	}
}

func TestProbeQueueDelayedReady(t *testing.T) {
	count := ptr.Int32(0)
	ts := newProbeTestServer(func(w http.ResponseWriter) {
		if atomic.AddInt32(count, 1) < 9 {
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

	timeout := 0
	if err := probeQueueHealthPath(timeout, port); err != nil {
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
