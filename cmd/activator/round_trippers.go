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

	h2cutil "github.com/knative/serving/pkg/h2c"
	"go.uber.org/zap"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (rt roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

// baseTransport will use the appropriate transport for the request's http protocol version
var baseTransport http.RoundTripper = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
	var transport http.RoundTripper = http.DefaultTransport
	if r.ProtoMajor == 2 {
		transport = h2cutil.DefaultTransport
	}

	return transport.RoundTrip(r)
})

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

func (rrt *retryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := rrt.transport.RoundTrip(r)
	// TODO: Activator should retry with backoff.
	// https://github.com/knative/serving/issues/1229
	i := 1
	for ; i < rrt.maxRetries; i++ {
		if err == nil {
			break
		}

		rrt.logger.Errorf("Error making a request: %s", err)

		time.Sleep(rrt.interval)

		resp, err = rrt.transport.RoundTrip(r)
	}

	// TODO: add metrics for number of tries and the response code.
	if resp != nil {
		rrt.logger.Infof("It took %d tries to get response code %d", i, resp.StatusCode)
	}
	return resp, err
}
