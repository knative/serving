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

package tracing

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/tracing/config"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	reporterrecorder "github.com/openzipkin/zipkin-go/reporter/recorder"
)

type fakeWriter struct {
	lastWrite *[]byte
}

func (fw fakeWriter) Header() http.Header {
	return http.Header{}
}

func (fw fakeWriter) Write(data []byte) (int, error) {
	*fw.lastWrite = data
	return len(data), nil
}

func (fw fakeWriter) WriteHeader(statusCode int) {
}

type testHandler struct {
}

func (th *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte{'f', 'a', 'k', 'e'})
}

func TestHTTPSpanMiddleware(t *testing.T) {
	cfg := config.Config{
		Enable: true,
		Debug:  true,
	}

	reporter := reporterrecorder.NewReporter()
	defer reporter.Close()

	repGetter := func(cfg *config.Config) (zipkinreporter.Reporter, error) {
		return reporter, nil
	}
	tc := TracerCache{
		CreateReporter: repGetter,
	}

	trGetter := func(ctx context.Context) (*TracerRef, error) {
		return tc.NewTracerRef(&cfg, "test-service", "localhost:1234")
	}

	next := testHandler{}
	middleware := HTTPSpanMiddleware(nil, "test-op", trGetter, &next)

	var lastWrite []byte
	fw := fakeWriter{lastWrite: &lastWrite}

	req, err := http.NewRequest("GET", "http://test.example.com", nil)
	if err != nil {
		t.Errorf("Failed to make fake request: %v", err)
	}
	middleware.ServeHTTP(fw, req)

	// Assert our next handler was called
	if diff := cmp.Diff([]byte("fake"), lastWrite); diff != "" {
		t.Errorf("Got http response (-want, +got) = %v", diff)
	}

	spans := reporter.Flush()
	if len(spans) != 1 {
		t.Errorf("Got %d spans, expected 1", len(spans))
	}
}
