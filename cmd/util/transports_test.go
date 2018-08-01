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
	"errors"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestHttpRoundTripper(t *testing.T) {
	wants := map[string]bool{}
	frt := func(key string) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			wants[key] = true

			return nil, nil
		})
	}

	rt := NewHttpTransport(frt("v1"), frt("v2"))

	examples := []struct {
		label      string
		protoMajor int
		want       string
	}{
		{
			label:      "use default transport for http1",
			protoMajor: 1,
			want:       "v1",
		},
		{
			label:      "use h2c transport for http2",
			protoMajor: 2,
			want:       "v2",
		},
		{
			label:      "use default transport for all others",
			protoMajor: 99,
			want:       "v1",
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			wants[e.want] = false

			r := &http.Request{ProtoMajor: e.protoMajor}

			rt.RoundTrip(r)

			if wants[e.want] != true {
				t.Error("Wrong transport selected for request.")
			}
		})
	}
}

func TestRetryRoundTripper(t *testing.T) {
	req := &http.Request{}

	goodStatus := 200
	badStatus1 := 500
	badStatus2 := 400

	resp := func(status int) *http.Response {
		return &http.Response{StatusCode: status, Body: &SpyCloser{}}
	}

	someErr := errors.New("some error")

	logger := zap.NewExample().Sugar()
	conditions := []RetryCond{RetryStatus(badStatus1), RetryStatus(badStatus2)}

	examples := []struct {
		label          string
		resp           *http.Response
		err            error
		cond           []RetryCond
		wantRetry      bool
		wantBodyClosed bool
	}{
		{
			label:          "no retry",
			resp:           resp(goodStatus),
			err:            nil,
			cond:           conditions,
			wantRetry:      false,
			wantBodyClosed: false,
		},
		{
			label:          "no conditions",
			resp:           resp(badStatus1),
			err:            nil,
			cond:           []RetryCond{},
			wantRetry:      false,
			wantBodyClosed: false,
		},
		{
			label:          "retry on error",
			resp:           nil,
			err:            someErr,
			cond:           conditions,
			wantRetry:      true,
			wantBodyClosed: false,
		},
		{
			label:          "retry on condition 1",
			resp:           resp(badStatus1),
			err:            nil,
			cond:           conditions,
			wantRetry:      true,
			wantBodyClosed: true,
		},
		{
			label:          "retry on condition 2",
			resp:           resp(badStatus2),
			err:            nil,
			cond:           conditions,
			wantRetry:      true,
			wantBodyClosed: true,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			transport := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return e.resp, e.err
			})

			retry := RetryerFunc(func(a func() bool) int {
				if a() {
					if !e.wantRetry {
						t.Errorf("Unexpected retry.")
					}
				}
				return 1
			})

			rt := NewRetryRoundTripper(transport, logger, retry, e.cond...)

			gotResp, gotErr := rt.RoundTrip(req)

			if gotResp != e.resp {
				t.Errorf("Unexpected response. Want %v, got %v", e.resp, gotResp)
			}

			if gotErr != e.err {
				t.Errorf("Unexpected error. Want %v, got %v", e.err, gotErr)
			}

			if e.wantBodyClosed && !e.resp.Body.(*SpyCloser).Closed {
				t.Errorf("Expected response body to be closed.")
			}
		})
	}
}

func TestLinearRetry(t *testing.T) {
	checkInterval := func(last *time.Time, want time.Duration) {
		now := time.Now()
		got := now.Sub(*last)
		*last = now

		if got < want {
			t.Errorf("Unexpected retry interval. Want %v, got %v", want, got)
		}
	}

	examples := []struct {
		label       string
		interval    time.Duration
		maxRetries  int
		responses   []bool
		wantRetries int
	}{
		{
			label:       "atleast once",
			interval:    5 * time.Millisecond,
			maxRetries:  0,
			responses:   []bool{true},
			wantRetries: 1,
		},
		{
			label:       "< maxRetries",
			interval:    5 * time.Millisecond,
			maxRetries:  3,
			responses:   []bool{false, true},
			wantRetries: 2,
		},
		{
			label:       "= maxRetries",
			interval:    10 * time.Millisecond,
			maxRetries:  3,
			responses:   []bool{false, false, true},
			wantRetries: 3,
		},
		{
			label:       "> maxRetries",
			interval:    5 * time.Millisecond,
			maxRetries:  3,
			responses:   []bool{false, false, false, true},
			wantRetries: 3,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			var lastRetry time.Time
			var got int

			a := func() bool {
				checkInterval(&lastRetry, e.interval)

				ok := e.responses[got]
				got++

				return ok
			}

			lr := NewLinearRetryer(e.interval, e.maxRetries)

			reported := lr.Retry(a)

			if got != e.wantRetries {
				t.Errorf("Unexpected retries. Want %d, got %d", e.wantRetries, got)
			}

			if reported != e.wantRetries {
				t.Errorf("Unexpected retries reported. Want %d, got %d", e.wantRetries, reported)
			}
		})
	}
}
