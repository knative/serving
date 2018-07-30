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
	"fmt"
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

// statusFilterRoundTripper returns an error if the response contains one of the filtered statuses.
type statusFilterRoundTripper struct {
	transport http.RoundTripper
	statuses  []int
}

func newStatusFilterRoundTripper(rt http.RoundTripper, statuses ...int) http.RoundTripper {
	return &statusFilterRoundTripper{rt, statuses}
}

func (rt *statusFilterRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := rt.transport.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	for _, status := range rt.statuses {
		if resp.StatusCode == status {
			resp.Body.Close()

			return nil, fmt.Errorf("Filtering %d", status)
		}
	}

	return resp, nil
}

// retryRoundTripper retries a request on error up to `maxRetries` times,
// waiting `interval` milliseconds between retries
type retryRoundTripper struct {
	logger     *zap.SugaredLogger
	maxRetries int
	interval   time.Duration
	transport  http.RoundTripper
}

func newRetryRoundTripper(rt http.RoundTripper, l *zap.SugaredLogger, mr int, i time.Duration) http.RoundTripper {
	return &retryRoundTripper{logger: l, maxRetries: mr, interval: i, transport: rt}
}

func (rrt *retryRoundTripper) RoundTrip(r *http.Request) (resp *http.Response, err error) {
	attempts := rrt.retry(func() bool {
		resp, err = rrt.transport.RoundTrip(r)
		if err != nil {
			rrt.logger.Errorf("Error making a request: %s", err)

			return true
		}

		return false
	})

	// TODO: add metrics for number of tries and the response code.
	if err == nil {
		rrt.logger.Infof("Activation finished after %d attempt(s). Response code: %d", attempts, resp.StatusCode)
	} else {
		rrt.logger.Errorf("Activation failed after %d attempts. Last error: %v", attempts, err)
	}

	return
}

func (rrt *retryRoundTripper) retry(action func() bool) (attempts int) {
	// TODO: Activator should retry with backoff.
	// https://github.com/knative/serving/issues/1229
	for attempts = 1; attempts <= rrt.maxRetries && action(); attempts++ {
		time.Sleep(rrt.interval)
	}

	return
}
