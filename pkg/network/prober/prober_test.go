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

	"github.com/knative/serving/pkg/network"
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
		status      int
		body        string
	}{{
		name:        "ok",
		headerValue: systemName,
		status:      http.StatusOK,
		body:        systemName,
	}, {
		name:        "wrong system",
		headerValue: "bells-and-whistles",
		status:      http.StatusBadRequest,
		body:        unexpectedProbeMessage,
	}, {
		name:        "no header",
		headerValue: "",
		status:      http.StatusNotFound,
		body:        "",
	}}
	for _, test := range tests {
		st, body, err := Do(context.Background(), ts.URL, test.headerValue)
		if got, want := st, test.status; got != want {
			t.Errorf("Status = %v, want: %v", got, want)
		}
		if got, want := body, test.body; got != want {
			t.Errorf("Body = %q, want: %q", got, want)
		}
		if err != nil {
			t.Errorf("Do returned error: %v", err)
		}
	}
}

func TestBlackHole(t *testing.T) {
	st, body, err := Do(context.Background(), "http://gone.fishing.svc.custer.local:8080", systemName)
	if got, want := st, 0; got != want {
		t.Errorf("Status = %v, want: %v", got, want)
	}
	if got, want := body, ""; got != want {
		t.Errorf("Body = %q, want: %q", got, want)
	}
	if err == nil {
		t.Error("Do did not return an error")
	}
}

func TestBadURL(t *testing.T) {
	_, _, err := Do(context.Background(), ":foo", systemName)
	if err == nil {
		t.Error("Do did not return an error")
	}
	t.Logf("For the curious the error was: %v", err)
}
