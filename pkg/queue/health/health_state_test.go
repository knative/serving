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

package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"knative.dev/serving/pkg/queue"
)

func TestHealthStateSetsState(t *testing.T) {
	s := NewState()

	wantAlive := func() {
		if !s.isAlive() {
			t.Error("State was not alive but it should have been alive")
		}
	}
	wantNotAlive := func() {
		if s.isAlive() {
			t.Error("State was alive but it shouldn't have been")
		}
	}
	wantShuttingDown := func() {
		if !s.isShuttingDown() {
			t.Error("State was not shutting down but it should have been")
		}
	}
	wantNotShuttingDown := func() {
		if s.isShuttingDown() {
			t.Error("State was shutting down but it shouldn't have been")
		}
	}

	wantNotAlive()
	wantNotShuttingDown()

	s.setAlive()
	wantAlive()
	wantNotShuttingDown()

	s.shutdown()
	wantNotAlive()
	wantShuttingDown()
}

func TestHealthStateHealthHandler(t *testing.T) {
	tests := []struct {
		name         string
		alive        bool
		shuttingDown bool
		prober       func() bool
		isAggressive bool
		wantStatus   int
		wantBody     string
	}{{
		name:         "alive: true, K-Probe",
		alive:        true,
		isAggressive: false,
		wantStatus:   http.StatusOK,
		wantBody:     queue.Name,
	}, {
		name:         "alive: false, prober: true, K-Probe",
		prober:       func() bool { return true },
		isAggressive: false,
		wantStatus:   http.StatusOK,
		wantBody:     queue.Name,
	}, {
		name:         "alive: false, prober: false, K-Probe",
		prober:       func() bool { return false },
		isAggressive: false,
		wantStatus:   http.StatusServiceUnavailable,
	}, {
		name:         "alive: false, no prober, K-Probe",
		isAggressive: false,
		wantStatus:   http.StatusOK,
		wantBody:     queue.Name,
	}, {
		name:         "shuttingDown: true, K-Probe",
		shuttingDown: true,
		isAggressive: false,
		wantStatus:   http.StatusGone,
	}, {
		name:         "no prober, shuttingDown: false",
		isAggressive: true,
		wantStatus:   http.StatusOK,
		wantBody:     queue.Name,
	}, {
		name:         "prober: true, shuttingDown: true",
		shuttingDown: true,
		prober:       func() bool { return true },
		isAggressive: true,
		wantStatus:   http.StatusGone,
	}, {
		name:         "prober: true, shuttingDown: false",
		prober:       func() bool { return true },
		isAggressive: true,
		wantStatus:   http.StatusOK,
		wantBody:     queue.Name,
	}, {
		name:         "prober: false, shuttingDown: false",
		prober:       func() bool { return false },
		isAggressive: true,
		wantStatus:   http.StatusServiceUnavailable,
	}, {
		name:         "prober: false, shuttingDown: true",
		shuttingDown: true,
		prober:       func() bool { return false },
		isAggressive: true,
		wantStatus:   http.StatusGone,
	}, {
		name:         "alive: true, prober: false, shuttingDown: false",
		alive:        true,
		prober:       func() bool { return false },
		isAggressive: true,
		wantStatus:   http.StatusServiceUnavailable,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := NewState()
			state.alive = test.alive
			state.shuttingDown = test.shuttingDown

			rr := httptest.NewRecorder()
			state.HandleHealthProbe(test.prober, test.isAggressive, rr)

			if got, want := rr.Code, test.wantStatus; got != want {
				t.Errorf("handler returned wrong status code: got %v want %v", got, want)
			}

			if got, want := rr.Body.String(), test.wantBody; got != want {
				t.Errorf("handler returned unexpected body: got %v want %v", got, want)
			}
		})
	}
}

func TestHealthStateDrainHandler(t *testing.T) {
	state := NewState()
	state.setAlive()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	completedCh := make(chan struct{}, 1)
	handler := http.HandlerFunc(state.DrainHandlerFunc())
	go func(handler http.Handler, recorder *httptest.ResponseRecorder) {
		handler.ServeHTTP(recorder, req)
		close(completedCh)
	}(handler, rr)

	state.drainFinished()
	<-completedCh

	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			rr.Code, http.StatusOK)
	}
}

func TestHealthStateDrainHandlerNotRacy(t *testing.T) {
	state := NewState()
	state.setAlive()

	// Complete the drain _before_ the DrainHandlerFunc is attached.
	state.drainFinished()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	completedCh := make(chan struct{}, 1)
	handler := http.HandlerFunc(state.DrainHandlerFunc())
	go func(handler http.Handler, recorder *httptest.ResponseRecorder) {
		handler.ServeHTTP(recorder, req)
		close(completedCh)
	}(handler, rr)

	select {
	case <-completedCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for drain handler to return")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			rr.Code, http.StatusOK)
	}
}

func TestHealthStateShutdown(t *testing.T) {
	state := NewState()
	state.setAlive()

	calledCh := make(chan struct{}, 1)
	state.Shutdown(func() {
		close(calledCh)
	})

	// The channel should be closed as the cleaner is called.
	select {
	case <-calledCh:
	case <-time.After(2 * time.Second):
		t.Error("drain function not called when shutting down")
	}

	if !state.drainCompleted {
		t.Error("shutdown did not complete draining")
	}

	if !state.shuttingDown {
		t.Errorf("wrong shutdown state: got %v want %v", state.shuttingDown, true)
	}

	if state.alive {
		t.Errorf("wrong alive state: got %v want %v", state.alive, false)
	}
}
