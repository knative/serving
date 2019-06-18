/*
Copyright 2019 The Knative Authors

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

package prober

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/knative/serving/pkg/network"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	systemName             = "test-server"
	unexpectedProbeMessage = "unexpected probe header value: whatever"
)

func probeServeFunc(w http.ResponseWriter, r *http.Request) {
	s := r.Header.Get(network.ProbeHeaderName)
	switch s {
	case "":
		// No header.
		w.WriteHeader(http.StatusNotFound)
	case systemName:
		// Expected header value.
		w.Write([]byte(systemName))
	default:
		// Unexpected header value.
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(unexpectedProbeMessage))
	}
}

func TestDoServing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(probeServeFunc))
	defer ts.Close()
	tests := []struct {
		name        string
		headerValue string
		want        bool
	}{{
		name:        "ok",
		headerValue: systemName,
		want:        true,
	}, {
		name:        "wrong system",
		headerValue: "bells-and-whistles",
		want:        false,
	}, {
		name:        "no header",
		headerValue: "",
		want:        false,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Do(context.Background(), network.NewAutoTransport(), ts.URL, test.headerValue)
			if want := test.want; got != want {
				t.Errorf("Got = %v, want: %v", got, want)
			}
			if err != nil {
				t.Errorf("Do returned error: %v", err)
			}
		})
	}
}

func TestBlackHole(t *testing.T) {
	got, err := Do(context.Background(), network.NewAutoTransport(), "http://gone.fishing.svc.custer.local:8080", systemName)
	if want := false; got != want {
		t.Errorf("Got = %v, want: %v", got, want)
	}
	if err == nil {
		t.Error("Do did not return an error")
	}
}

func TestBadURL(t *testing.T) {
	_, err := Do(context.Background(), network.NewAutoTransport(), ":foo", systemName)
	if err == nil {
		t.Error("Do did not return an error")
	}
	t.Logf("For the curious the error was: %v", err)
}

func TestDoAsync(t *testing.T) {
	// This replicates the TestDo.
	ts := httptest.NewServer(http.HandlerFunc(probeServeFunc))
	defer ts.Close()

	wch := make(chan interface{})
	defer close(wch)
	tests := []struct {
		name        string
		headerValue string
		cb          Done
	}{{
		name:        "ok",
		headerValue: systemName,
		cb: func(arg interface{}, ret bool, err error) {
			defer func() {
				wch <- 42
			}()
			if got, want := arg.(string), "ok"; got != want {
				t.Errorf("arg = %s, want: %s", got, want)
			}
			if !ret {
				t.Error("result was false")
			}
		},
	}, {
		name:        "wrong system",
		headerValue: "bells-and-whistles",
		cb: func(arg interface{}, ret bool, err error) {
			defer func() {
				wch <- 1984
			}()
			if ret {
				t.Error("result was true")
			}
		},
	}, {
		name:        "no header",
		headerValue: "",
		cb: func(arg interface{}, ret bool, err error) {
			defer func() {
				wch <- 2006
			}()
			if ret {
				t.Error("result was true")
			}
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := New(test.cb, network.NewAutoTransport)
			m.Offer(context.Background(), ts.URL, test.headerValue, test.name, 50*time.Millisecond, 2*time.Second)
			<-wch
		})
	}
}

type thirdTimesTheCharmProber struct {
	calls int
}

func (t *thirdTimesTheCharmProber) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.calls++
	if t.calls < 3 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(unexpectedProbeMessage))
		return
	}
	w.Write([]byte(systemName))
}

func TestDoAsyncRepeat(t *testing.T) {
	c := &thirdTimesTheCharmProber{}
	ts := httptest.NewServer(c)
	defer ts.Close()

	wch := make(chan interface{})
	defer close(wch)
	cb := func(arg interface{}, done bool, err error) {
		if !done {
			t.Error("done was false")
		}
		if err != nil {
			t.Errorf("Unexpected error = %v", err)
		}
		wch <- arg
	}
	m := New(cb, network.NewAutoTransport)
	m.Offer(context.Background(), ts.URL, systemName, 42, 50*time.Millisecond, 3*time.Second)
	<-wch
	if got, want := c.calls, 3; got != want {
		t.Errorf("Probe invocation count = %d, want: %d", got, want)
	}
}

func TestDoAsyncTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	wch := make(chan interface{})
	defer close(wch)

	cb := func(arg interface{}, done bool, err error) {
		if err != wait.ErrWaitTimeout {
			t.Errorf("Unexpected error = %v", err)
		}
		wch <- arg
	}
	m := New(cb, network.NewAutoTransport)
	m.Offer(context.Background(), ts.URL, systemName, 2009, 10*time.Millisecond, 200*time.Millisecond)
	<-wch
}

func TestAsyncMultiple(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(probeServeFunc))
	defer ts.Close()

	wch := make(chan interface{})
	defer close(wch)
	cb := func(arg interface{}, done bool, err error) {
		<-wch
		wch <- 2006
	}
	m := New(cb, network.NewAutoTransport)
	if !m.Offer(context.Background(), ts.URL, systemName, 1984, 100*time.Millisecond, 1*time.Second) {
		t.Error("First call to offer returned false")
	}
	if m.Offer(context.Background(), ts.URL, systemName, 1982, 100*time.Millisecond, 1*time.Second) {
		t.Error("Second call to offer returned true")
	}
	if got, want := m.len(), 1; got != want {
		t.Errorf("Number of queued items = %d, want: %d", got, want)
	}
	// Make sure we terminate the first probe.
	wch <- 2009
	<-wch
	// ಠ_ಠ gotta wait for the cb to end.
	time.Sleep(300 * time.Millisecond)
	if got, want := m.len(), 0; got != want {
		t.Errorf("Number of queued items = %d, want: %d", got, want)
	}
}

func (m *Manager) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keys.Len()
}
