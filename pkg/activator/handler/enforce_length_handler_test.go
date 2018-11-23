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
package handler

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnforceMaxContentLengthHandler(t *testing.T) {
	payload := "SAMPLE PAYLOAD"

	// httptest.NewRequest will set ContentLength for strings.NewReader
	fixed := func(p string) io.Reader { return strings.NewReader(p) }
	stream := func(p string) io.Reader { return ioutil.NopCloser(strings.NewReader(p)) }

	examples := []struct {
		label     string
		maxUpload int
		request   io.Reader
		response  string
		status    int
	}{
		{
			label:     "under with ContentLength",
			maxUpload: len(payload) + 1,
			request:   fixed(payload),
			status:    http.StatusOK,
			response:  payload,
		},
		{
			label:     "equal with ContentLength",
			maxUpload: len(payload),
			request:   fixed(payload),
			status:    http.StatusOK,
			response:  payload,
		},
		{
			label:     "over with ContentLength",
			maxUpload: len(payload) - 1,
			request:   fixed(payload),
			status:    http.StatusRequestEntityTooLarge,
			response:  "",
		},
		{
			label:     "under without ContentLength",
			maxUpload: len(payload) + 1,
			request:   stream(payload),
			status:    http.StatusOK,
			response:  payload,
		},
		{
			label:     "equal without ContentLength",
			maxUpload: len(payload),
			request:   stream(payload),
			status:    http.StatusOK,
			response:  payload,
		},
		{
			label:     "over without ContentLength",
			maxUpload: len(payload) - 1,
			request:   stream(payload),
			status:    http.StatusOK,
			response:  payload[0 : len(payload)-1],
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			// Return 200 and request body
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)

				if r.Body != nil {
					body, err := ioutil.ReadAll(r.Body)
					if err != nil {
						t.Fatalf("Error request reading body")
					}
					w.Write(body)
				}
			})
			handler := EnforceMaxContentLengthHandler{NextHandler: baseHandler, MaxContentLengthBytes: int64(e.maxUpload)}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "http://example.com", e.request)

			handler.ServeHTTP(resp, req)

			gotBody, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Error request reading body")
			}

			if e.response != string(gotBody) {
				t.Errorf("Unexpected response body. Want %q, got %q", e.response, gotBody)
			}

			if resp.Code != e.status {
				t.Errorf("Unexpected response status. Want %d, got %d", e.status, resp.Code)
			}
		})
	}
}
