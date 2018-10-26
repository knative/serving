/*
Copyright 2018 The Knative Authors

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

package util

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func TestHeaderPruningNew(t *testing.T) {
	testHeaderName := "knative-test-header"
	testHeaderValue := "some-value"

	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		// These headers should be removed and not reach the client.
		for _, header := range responseHeadersToRemove {
			w.Header().Add(header, "foo")
		}

		// This header should not be removed
		w.Header().Add(testHeaderName, testHeaderValue)

		// These headers should've been stripped from the request.
		for _, header := range requestHeadersToRemove {
			if r.Header.Get(header) != "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
	}

	server := httptest.NewServer(handler)
	serverURL, _ := url.Parse(server.URL)

	defer server.Close()

	proxy := httputil.NewSingleHostReverseProxy(serverURL)

	// Adding a modifier, which should be preserved.
	testBodyContent := "body"
	proxy.ModifyResponse = func(r *http.Response) error {
		r.Body = ioutil.NopCloser(bytes.NewReader([]byte(testBodyContent)))
		return nil
	}
	SetupHeaderPruning(proxy)

	resp := httptest.NewRecorder()

	req := httptest.NewRequest("POST", "http://example.com", nil)
	for _, header := range requestHeadersToRemove {
		req.Header.Set(header, testHeaderValue)
	}

	proxy.ServeHTTP(resp, req)

	// The testserver returns BadRequest if any of those is set.
	if resp.Code != http.StatusOK {
		t.Errorf("expected request headers to be filtered")
	}

	for _, header := range responseHeadersToRemove {
		if resp.Header().Get(header) != "" {
			t.Errorf("expected header %q to be filtered", header)
		}
	}

	if resp.Header().Get(testHeaderName) != testHeaderValue {
		t.Errorf("expected header %q to not be stripped", testHeaderName)
	}

	if resp.Body.String() != testBodyContent {
		t.Error("expected the response modifier to be preserved")
	}
}
