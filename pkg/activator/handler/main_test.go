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
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync"
	"testing"

	netprobe "knative.dev/networking/pkg/http/probe"
	"knative.dev/pkg/logging"
	pkgnet "knative.dev/pkg/network"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator"
	apiconfig "knative.dev/serving/pkg/apis/config"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	pkghttp "knative.dev/serving/pkg/http"
)

// BenchmarkHandlerChain is supposed to try to project the entire handler chain of the
// activator to enable us to see improvements that span handlers and to judge some of
// the handlers that are not developed here.
func BenchmarkHandlerChain(b *testing.B) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(b)
	b.Cleanup(cancel)

	logger := logging.FromContext(ctx)
	configStore := setupConfigStore(b, logger)
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
			Body:       io.NopCloser(bytes.NewReader(body)),
			StatusCode: http.StatusOK,
		}, nil
	})

	// Make sure to update this if the activator's main file changes.
	ah := New(ctx, fakeThrottler{}, rt, false, logger, false /* TLS */)
	ah = concurrencyReporter.Handler(ah)
	ah = NewTracingHandler(ah)
	ah, _ = pkghttp.NewRequestLogHandler(ah, io.Discard, "", nil, false)
	ah = NewMetricHandler(activatorPodName, ah)
	ah = NewContextHandler(ctx, ah, configStore)
	ah = &ProbeHandler{NextHandler: ah}
	ah = netprobe.NewHandler(ah)
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

// TestActivatorChainHandlerWithFullDuplex tests activator's chain handler with the new http1 full duplex support against the issue
// https://github.com/golang/go/issues/40747, where reverse proxy failed with read errors.
// The test uses the reproducer in https://github.com/golang/go/issues/40747#issuecomment-733552061.
// We enable full duplex by setting the annotation `features.knative.dev/http-full-duplex` at the revision level.
func TestActivatorChainHandlerWithFullDuplex(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Testing this on Mac requires to loosen some restrictions, see https://github.com/knative/serving/pull/14568#issuecomment-1893151202 for more")
	}

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	rev := revision(testNamespace, testRevName)
	defer reset()
	rev.Annotations = map[string]string{apiconfig.AllowHTTPFullDuplexFeatureKey: "Enabled"}
	t.Cleanup(cancel)

	logger := logging.FromContext(ctx)
	configStore := setupConfigStore(t, logger)
	revisionInformer(ctx, rev)

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

	// The server responding with the sent body.
	echoServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				log.Printf("error reading body: %v", err)
				http.Error(w, fmt.Sprintf("error reading body: %v", err), http.StatusInternalServerError)
				return
			}

			if _, err := w.Write(body); err != nil {
				log.Printf("error writing body: %v", err)
			}
		},
	))
	defer echoServer.Close()

	// The server proxying requests to the echo server.
	echoURL, err := url.Parse(echoServer.URL)
	if err != nil {
		t.Fatalf("Failed to parse echo URL: %v", err)
	}

	proxy := pkghttp.NewHeaderPruningReverseProxy(echoURL.Host, "", []string{}, false)
	proxy.FlushInterval = 0
	proxyWithMiddleware := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})
	var ah http.Handler
	ah = concurrencyReporter.Handler(proxyWithMiddleware)
	ah = NewTracingHandler(ah)
	ah, _ = pkghttp.NewRequestLogHandler(ah, io.Discard, "", nil, false)
	ah = NewMetricHandler(activatorPodName, ah)
	ah = WrapActivatorHandlerWithFullDuplex(ah, logger)
	ah = NewContextHandler(ctx, ah, configStore)
	ah = &ProbeHandler{NextHandler: ah}
	ah = netprobe.NewHandler(ah)
	ah = &HealthHandler{HealthCheck: func() error { return nil }, NextHandler: ah, Logger: logger}

	bodySize := 32 * 1024
	parallelism := 32

	proxyServer := httptest.NewServer(ah)

	defer proxyServer.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 10
	transport.MaxIdleConns = 100

	// Turning on this will hide the issue
	transport.DisableKeepAlives = false
	c := &http.Client{
		Transport: transport,
	}

	body := make([]byte, bodySize)
	for i := 0; i < cap(body); i++ {
		body[i] = 42
	}

	for i := 0; i < 10; i++ {
		var wg sync.WaitGroup
		wg.Add(parallelism)
		for i := 0; i < parallelism; i++ {
			go func(i int) {
				defer wg.Done()

				for i := 0; i < 100; i++ {
					if err := send(c, proxyServer.URL, body, "test-host"); err != nil {
						t.Errorf("error during request: %v", err)
					}
				}
			}(i)
		}

		wg.Wait()
	}

}

func send(client *http.Client, url string, body []byte, rHost string) error {
	r := bytes.NewBuffer(body)
	req, err := http.NewRequest("POST", url, r)

	if rHost != "" {
		req.Host = rHost
	}

	req.Header.Set(activator.RevisionHeaderNamespace, testNamespace)
	req.Header.Set(activator.RevisionHeaderName, testRevName)

	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	bd := io.Reader(resp.Body)

	rec, err := io.ReadAll(bd)

	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}

	if _, err = io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("failed to discard body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if len(rec) != len(body) {
		return fmt.Errorf("unexpected body length: %d", len(rec))
	}

	return nil
}
