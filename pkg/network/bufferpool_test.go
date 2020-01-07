/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package network

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	pkgnet "knative.dev/pkg/network"
)

func TestBufferPool(t *testing.T) {
	pool := NewBufferPool()
	// Transparently creates a new buffer
	buf := pool.Get()
	if got, want := len(buf), 32*1024; got != want {
		t.Errorf("len(buf) = %d, want %d", got, want)
	}
}

// TestBufferPoolInReverseProxy asserts correctness of the pool in context
// of the httputil.ReverseProxy. It makes sure that the behavior around
// slice "cleanup" is correct and that slices returned to the pool do not
// pollute later requests.
func TestBufferPoolInReverseProxy(t *testing.T) {
	want := "The testmessage successfully made its way through the roundtripper."

	url := &url.URL{}
	proxy := httputil.NewSingleHostReverseProxy(url)

	pool := NewBufferPool()
	proxy.BufferPool = pool
	proxy.Transport = pkgnet.RoundTripperFunc(func(*http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		recorder.WriteString(want)
		return recorder.Result(), nil
	})

	pool.Put([]byte("I'm polluting this pool with a buffer that's not empty."))
	pool.Put([]byte("I'm adding a little less."))
	pool.Put([]byte("And I'm even adding more info than the first message did, for sanity."))

	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, url.String(), nil)
	proxy.ServeHTTP(recorder, req)

	if got := recorder.Body.String(); got != want {
		t.Errorf("res.Body = %s, want %s", got, want)
	}
}
