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

package main

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// httpRoundTripper will use the appropriate transport for the request's http protocol version
type httpRoundTripper struct {
	v1 http.RoundTripper
	v2 http.RoundTripper
}

func newHttpRoundTripper(v1 http.RoundTripper, v2 http.RoundTripper) http.RoundTripper {
	return &httpRoundTripper{v1: v1, v2: v2}
}

func (rt *httpRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	var t http.RoundTripper = rt.v1
	if r.ProtoMajor == 2 {
		t = rt.v2
	}

	return t.RoundTrip(r)
}

type retryer func(func() bool) int

func linearRetryer(interval time.Duration, maxRetries int) retryer {
	return func(action func() bool) (retries int) {
		for retries = 1; !action() && retries < maxRetries; retries++ {
			time.Sleep(interval)
		}
		return
	}
}

type shouldRetryFunc func(*http.Response) bool

func retry503(resp *http.Response) bool {
	return resp.StatusCode == http.StatusServiceUnavailable
}

type retryRoundTripper struct {
	logger      *zap.SugaredLogger
	transport   http.RoundTripper
	retry       retryer
	shouldRetry shouldRetryFunc
}

// retryRoundTripper retries a request on error or `shouldRetry` condition, using the given `retry` strategy
func newRetryRoundTripper(rt http.RoundTripper, l *zap.SugaredLogger, r retryer, sr shouldRetryFunc) http.RoundTripper {
	return &retryRoundTripper{
		logger:      l,
		transport:   rt,
		retry:       r,
		shouldRetry: sr,
	}
}

func (rrt *retryRoundTripper) RoundTrip(r *http.Request) (resp *http.Response, err error) {
	attempts := rrt.retry(func() bool {
		resp, err = rrt.transport.RoundTrip(r)

		if err != nil {
			rrt.logger.Errorf("Error making a request: %s", err)
			return true
		}

		if rrt.shouldRetry(resp) {
			resp.Body.Close()
			return true
		}

		return false
	})

	// TODO: add metrics for number of tries and the response code.
	if err == nil {
		rrt.logger.Infof("Finished after %d attempt(s). Response code: %d", attempts, resp.StatusCode)
	} else {
		rrt.logger.Errorf("Failed after %d attempts. Last error: %v", attempts, err)
	}

	return
}
