//go:build e2e
// +build e2e

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

package e2e

import (
	"net/http"
	"testing"

	"knative.dev/pkg/test/spoof"
)

func TestIsEgressResponse(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		location string
		wantErr  bool
	}{
		{name: "ok", status: http.StatusOK},
		{name: "moved permanently", status: http.StatusMovedPermanently, location: "https://www.google.com/"},
		{name: "anti-bot redirect", status: http.StatusFound, location: "http://www.google.com/sorry/index?continue=http://www.google.com/"},
		{name: "see other", status: http.StatusSeeOther, location: "https://www.google.com/"},
		{name: "temporary redirect", status: http.StatusTemporaryRedirect, location: "https://www.google.com/"},
		{name: "permanent redirect", status: http.StatusPermanentRedirect, location: "https://www.google.com/"},
		{name: "not found", status: http.StatusNotFound, wantErr: true},
		{name: "proxy failure", status: http.StatusBadGateway, wantErr: true},
		{name: "missing location", status: http.StatusFound, wantErr: true},
		{name: "malformed location", status: http.StatusFound, location: "%", wantErr: true},
		{name: "relative location", status: http.StatusFound, location: "/sorry/index", wantErr: true},
		{name: "ingress redirect", status: http.StatusMovedPermanently, location: "https://egress-traffic.serving-tests.example.com/", wantErr: true},
		{name: "unexpected host", status: http.StatusFound, location: "https://example.com/", wantErr: true},
		{name: "unexpected scheme", status: http.StatusFound, location: "ftp://www.google.com/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := isEgressResponse(&spoof.Response{
				StatusCode: tt.status,
				Header:     http.Header{"Location": []string{tt.location}},
			})
			if !done || (err != nil) != tt.wantErr {
				t.Errorf("isEgressResponse() = (%v, %v), want (true, error: %v)", done, err, tt.wantErr)
			}
		})
	}
}
