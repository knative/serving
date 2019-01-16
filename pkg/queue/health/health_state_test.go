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
)

const (
	aliveBody    = "alive: true"
	notAliveBody = "alive: false"
)

func assertAlive(s *State, t *testing.T) {
	if !s.IsAlive() {
		t.Error("State was not alive but it should have been alive")
	}
}

func assertNotAlive(s *State, t *testing.T) {
	if s.IsAlive() {
		t.Error("State was alive but it shouldn't have been")
	}
}

func assertShuttingDown(s *State, t *testing.T) {
	if !s.IsShuttingDown() {
		t.Error("State was not shutting down but it should have been")
	}
}

func assertNotShuttingDown(s *State, t *testing.T) {
	if s.IsShuttingDown() {
		t.Error("State was shutting down but it shouldn't have been")
	}
}

func TestHealthStateSetsState(t *testing.T) {
	s := &State{}
	assertNotAlive(s, t)
	assertNotShuttingDown(s, t)

	s.setAlive()
	assertAlive(s, t)
	assertNotShuttingDown(s, t)

	s.shutdown()
	assertNotAlive(s, t)
	assertShuttingDown(s, t)
}

func TestHealthStateHealthHandler(t *testing.T) {
	tests := []struct {
		name       string
		state      *State
		prober     func() bool
		wantStatus int
		wantBody   string
	}{{
		name:       "alive: true",
		state:      &State{alive: true},
		wantStatus: http.StatusOK,
		wantBody:   aliveBody,
	}, {
		name:       "alive: false, prober: true",
		state:      &State{alive: false},
		prober:     func() bool { return true },
		wantStatus: http.StatusOK,
		wantBody:   aliveBody,
	}, {
		name:       "alive: false, prober: false",
		state:      &State{alive: false},
		prober:     func() bool { return false },
		wantStatus: http.StatusBadRequest,
		wantBody:   notAliveBody,
	}, {
		name:       "alive: false, no prober",
		state:      &State{alive: false},
		wantStatus: http.StatusOK,
		wantBody:   aliveBody,
	}, {
		name:       "shuttingDown: true",
		state:      &State{shuttingDown: true},
		wantStatus: http.StatusBadRequest,
		wantBody:   notAliveBody,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(test.state.HealthHandler(test.prober))

			handler.ServeHTTP(rr, req)

			if rr.Code != test.wantStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					rr.Code, test.wantStatus)
			}

			if rr.Body.String() != test.wantBody {
				t.Errorf("handler returned unexpected body: got %v want %v",
					rr.Body.String(), test.wantBody)
			}
		})
	}
}

func TestHealthStateQuitHandler(t *testing.T) {
	state := &State{}
	state.setAlive()

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	calledCh := make(chan struct{}, 1)
	handler := http.HandlerFunc(state.QuitHandler(func() {
		close(calledCh)
	}))
	handler.ServeHTTP(rr, req)

	// The channel should be closed as the cleaner is called.
	<-calledCh

	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			rr.Code, http.StatusOK)
	}

	if rr.Body.String() != notAliveBody {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), notAliveBody)
	}
}
