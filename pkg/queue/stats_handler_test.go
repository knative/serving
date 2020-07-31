/*
Copyright 2020 The Knative Authors

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

package queue

import (
	"net/http"
	"net/http/httptest"
	"testing"

	network "knative.dev/networking/pkg"
)

func TestStatsHandler(t *testing.T) {
	prom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("served-by", "prometheus")
	})

	proto := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("served-by", "protobuf")
	})

	reporter := NewStatsHandler(prom, proto)

	tests := []struct {
		name    string
		headers http.Header
		expect  string
	}{{
		name:   "no headers",
		expect: "prometheus",
	}, {
		name: "some other accept header",
		headers: http.Header{
			"Accept": []string{"something-else"},
		},
		expect: "prometheus",
	}, {
		name: "protobuf in second Accept header still serves prometheus",
		headers: http.Header{
			"Accept": []string{"", network.ProtoAcceptContent},
		},
		expect: "prometheus",
	}, {
		name: "protobuf accept header",
		headers: http.Header{
			"Accept": []string{network.ProtoAcceptContent},
		},
		expect: "protobuf",
	}, {
		name: "protobuf accept header as part of CSV",
		headers: http.Header{
			"Accept": []string{"something/else, application/protobuf"},
		},
		expect: "protobuf",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			r.Header = test.headers

			w := httptest.NewRecorder()
			reporter.ServeHTTP(w, r)

			if got, want := w.Header().Get("served-by"), test.expect; got != want {
				t.Errorf("Expected to be served %s but was served %s", want, got)
			}
		})
	}
}
