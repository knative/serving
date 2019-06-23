/*
Copyright 2019 The Knative Authors

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
package queue

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardedShimHandler(t *testing.T) {
	tests := []struct {
		name string
		xff  string
		xfh  string
		xfp  string
		fwd  string
		want string
	}{{
		name: "multiple xff",
		xff:  "127.0.0.1, ::1",
		xfh:  "h",
		xfp:  "p",
		want: "for=127.0.0.1;host=h;proto=p, for=\"[::1]\"",
	}, {
		name: "single xff",
		xff:  "127.0.0.1",
		xfh:  "h",
		xfp:  "p",
		want: "for=127.0.0.1;host=h;proto=p",
	}, {
		name: "multiple xff, no xfh, no xfp",
		xff:  "127.0.0.1, ::1",
		want: "for=127.0.0.1, for=\"[::1]\"",
	}, {
		name: "multiple xff, no xfh",
		xff:  "127.0.0.1, ::1",
		xfp:  "p",
		want: "for=127.0.0.1;proto=p, for=\"[::1]\"",
	}, {
		name: "multiple xff, no xfp",
		xff:  "127.0.0.1, ::1",
		xfh:  "h",
		want: "for=127.0.0.1;host=h, for=\"[::1]\"",
	}, {
		name: "only xfh",
		xfh:  "h",
		want: "host=h",
	}, {
		name: "only xfp",
		xfp:  "p",
		want: "proto=p",
	}, {
		name: "only xfp and xfh",
		xfh:  "h",
		xfp:  "p",
		want: "host=h;proto=p",
	}, {
		name: "existing fwd",
		xff:  "127.0.0.1, ::1",
		xfh:  "h",
		xfp:  "p",
		fwd:  "for=a, for=b",
		want: "for=a, for=b",
	}, {
		name: "no xf* headers",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ""

			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			if test.xff != "" {
				req.Header.Set("X-Forwarded-For", test.xff)
			}
			if test.xfh != "" {
				req.Header.Set("X-Forwarded-Host", test.xfh)
			}
			if test.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", test.xfp)
			}
			if test.fwd != "" {
				req.Header.Set("Forwarded", test.fwd)
			}

			resp := httptest.NewRecorder()

			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = req.Header.Get("Forwarded")
			})

			ForwardedShimHandler(h).ServeHTTP(resp, req)

			if test.want != got {
				t.Errorf("Wrong header value. Want %q, got %q", test.want, got)
			}
		})
	}
}
