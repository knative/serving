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
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnforceMaxContentLengthHandler(t *testing.T) {
	payload := "SAMPLE PAYLOAD"

	examples := []struct {
		label     string
		maxUpload int
		status    int
	}{
		{
			label:     "under",
			maxUpload: len(payload) + 1,
			status:    http.StatusOK,
		},
		{
			label:     "equal",
			maxUpload: len(payload),
			status:    http.StatusOK,
		},
		{
			label:     "over",
			maxUpload: len(payload) - 1,
			status:    http.StatusRequestEntityTooLarge,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := EnforceMaxContentLengthHandler{NextHandler: baseHandler, MaxContentLengthBytes: int64(e.maxUpload)}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "http://example.com", bytes.NewBufferString(payload))

			handler.ServeHTTP(resp, req)

			if resp.Code != e.status {
				t.Errorf("Unexpected response status for payload %q. Want %d, got %d", payload, e.status, resp.Code)
			}
		})
	}
}
