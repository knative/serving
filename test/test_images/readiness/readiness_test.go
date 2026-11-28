/*
Copyright 2026 The Knative Authors

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
	"sync"
	"testing"
)

func TestBlockedStartupHealth(t *testing.T) {
	if err := initializeHealth(true, ""); err != nil {
		t.Fatal(err)
	}
	checkHealth := func(want int) {
		t.Helper()
		w := httptest.NewRecorder()
		handleReadiness(w, httptest.NewRequest(http.MethodGet, "/healthz/readiness", nil))
		if w.Code != want {
			t.Fatalf("health status = %d, want %d", w.Code, want)
		}
	}
	checkHealth(http.StatusInternalServerError)
	w := httptest.NewRecorder()
	handleStartSucceeding(w, httptest.NewRequest(http.MethodGet, "/start-succeeding", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET release = %d, Allow=%q", w.Code, w.Header().Get("Allow"))
	}
	checkHealth(http.StatusInternalServerError)
	for range 2 {
		w := httptest.NewRecorder()
		handleStartSucceeding(w, httptest.NewRequest(http.MethodPost, "/start-succeeding", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("POST release = %d", w.Code)
		}
		checkHealth(http.StatusOK)
	}
	if err := initializeHealth(true, ""); err != nil {
		t.Fatal(err)
	}
	checkHealth(http.StatusInternalServerError)
}

func TestHealthInitialization(t *testing.T) {
	if err := initializeHealth(true, "1s"); err == nil {
		t.Error("accepted blocked startup with READY_DELAY")
	}
	if err := initializeHealth(false, "invalid"); err == nil {
		t.Error("accepted invalid READY_DELAY")
	}
	if err := initializeHealth(false, ""); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleReadiness(w, httptest.NewRequest(http.MethodGet, "/healthz/readiness", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ordinary health status = %d", w.Code)
	}
}

func TestConcurrentHealthAccess(t *testing.T) {
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := initializeHealth(true, ""); err != nil {
				t.Error(err)
			}
			handleStartSucceeding(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/start-succeeding", nil))
			handleReadiness(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz/readiness", nil))
		})
	}
	wg.Wait()
}
