/*
Copyright 2021 The Knative Authors

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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/logging"
	pkgnet "knative.dev/pkg/network"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	pkghttp "knative.dev/serving/pkg/http"
)

// BenchmarkHandlerChain is supposed to try to project the entire handler chain of the
// activator to enable us to see improvements that span handlers and to judge some of
// the handlers that are not developed here.
func BenchmarkHandlerChain(b *testing.B) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(&testing.T{})
	b.Cleanup(cancel)

	logger := logging.FromContext(ctx)
	configStore := setupConfigStore(&testing.T{}, logger)
	revision := revision(testNamespace, testRevName)
	revisionInformer(ctx, revision)

	// Buffer equal to the activator.
	statCh := make(chan []asmetrics.StatMessage)
	concurrencyReporter := NewConcurrencyReporter(ctx, activatorPodName, statCh)
	go concurrencyReporter.Run(ctx.Done())

	// Just read and ignore all stat messages.
	go func() {
		for {
			select {
			case <-statCh:
			case <-ctx.Done():
				return
			}
		}
	}()

	body := []byte(randomString(1024))
	rt := pkgnet.RoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Body:       ioutil.NopCloser(bytes.NewReader(body)),
			StatusCode: http.StatusOK,
		}, nil
	})

	// Make sure to update this if the activator's main file changes.
	ah := New(ctx, fakeThrottler{}, rt, logger)
	ah = concurrencyReporter.Handler(ah)
	ah = NewTracingHandler(ah)
	ah, _ = pkghttp.NewRequestLogHandler(ah, ioutil.Discard, "", nil, false)
	ah = NewMetricHandler(activatorPodName, ah)
	ah = NewContextHandler(ctx, ah, configStore)
	ah = &ProbeHandler{NextHandler: ah}
	ah = network.NewProbeHandler(ah)
	ah = &HealthHandler{HealthCheck: func() error { return nil }, NextHandler: ah, Logger: logger}

	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
		req.Host = "test-host"
		req.Header.Set(activator.RevisionHeaderNamespace, testNamespace)
		req.Header.Set(activator.RevisionHeaderName, testRevName)
		return req
	}

	test := func(req *http.Request, b *testing.B) {
		resp := &responseRecorder{}
		ah.ServeHTTP(resp, req)
		if resp.code != http.StatusOK {
			b.Fatalf("resp.Code = %d, want: StatusOK(200)", resp.code)
		}
		if got, want := resp.size.Load(), int32(len(body)); got != want {
			b.Fatalf("|body| = %d, want = %d", got, want)
		}
	}

	b.Run("sequential", func(b *testing.B) {
		req := request()
		for j := 0; j < b.N; j++ {
			test(req, b)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			req := request()
			for pb.Next() {
				test(req, b)
			}
		})
	})
}
