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
	"net/http"
)

// uploadHandler wraps the provided handler with a request body that supports
// re-reading and prevents uploads larger than `maxUploadBytes`
type uploadHandler struct {
	http.Handler
	MaxUploadBytes int64
}

func NewUploadHandler(h http.Handler, maxUploadBytes int64) http.Handler {
	return &uploadHandler{
		Handler:        h,
		MaxUploadBytes: maxUploadBytes,
	}
}

func (h *uploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > h.MaxUploadBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	// The request body cannot be read multiple times for retries.
	// The workaround is to clone the request body into a byte reader
	// so the body can be read multiple times.
	r.Body = NewRewinder(r.Body)

	h.Handler.ServeHTTP(w, r)
}
