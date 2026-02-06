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

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHijackTrackerNoHijack(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://somehost.com", nil)

	h := &HijackTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	h.ServeHTTP(w, r)

	err := h.Drain(context.Background())
	if err != nil {
		t.Fatal("unexpected error while draining", err)
	}
}

func TestHijackTrackerConnectionHijacked(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://somehost.com", nil)

	inHandler := make(chan struct{})
	handlerWait := make(chan struct{})
	drainStarted := make(chan struct{})
	drainResult := make(chan error, 1)

	h := &HijackTracker{
		PollInterval: 10 * time.Millisecond,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(inHandler)
			<-handlerWait
		}),
	}

	go func() {
		h.ServeHTTP(w, r)
	}()

	go func() {
		<-inHandler
		close(drainStarted)
		drainResult <- h.Drain(context.Background())
	}()

	<-drainStarted
	close(handlerWait)

	var err error
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("Drain was not unblocked when the handler returned")
	case err = <-drainResult:
	}

	if err != nil {
		t.Fatal("unexpected error draining", err)
	}
}

func TestHijackTrackerConnectionHijackedTimeout(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://somehost.com", nil)

	inHandler := make(chan struct{})
	handlerWait := make(chan struct{})
	drainStarted := make(chan struct{})
	drainResult := make(chan error, 1)

	h := &HijackTracker{
		PollInterval: 10 * time.Millisecond,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(inHandler)
			<-handlerWait
		}),
	}

	go func() {
		h.ServeHTTP(w, r)
	}()

	go func() {
		<-inHandler
		close(drainStarted)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		drainResult <- h.Drain(ctx)
	}()

	<-drainStarted
	// note: this is defered to unblock the go-routine
	// to clean up the test
	defer close(handlerWait)

	var err error
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("Drain did not timeout")
	case err = <-drainResult:
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unexpected error draining", err)
	}
}
