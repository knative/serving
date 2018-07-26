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
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRetryRoundTripper(t *testing.T) {
	wantBody := "all good!"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(wantBody))
	}))
	l := zap.NewExample().Sugar()
	maxRetries := 3
	interval := 10 * time.Millisecond

	examples := []struct {
		label   string
		retries int
		wantErr bool
	}{
		{
			label:   "success",
			retries: maxRetries,
			wantErr: false,
		},
		{
			label:   "failure",
			retries: maxRetries + 1,
			wantErr: true,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			var last time.Time

			gotRetries := 0
			rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				gotRetries += 1

				now := time.Now()
				duration := now.Sub(last)
				if duration < interval {
					t.Errorf("Unexpected retry interval. Want %v, got %v", interval, duration)
				}
				last = now

				if gotRetries < e.retries {

					if r.Body != nil {
						ioutil.ReadAll(r.Body)
						r.Body.Close()
					}

					return nil, errors.New("some error!")
				}

				return http.DefaultTransport.RoundTrip(r)
			})

			rrt := newRetryRoundTripper(rt, l, maxRetries, interval)
			req := httptest.NewRequest("", ts.URL, nil)

			resp, err := rrt.RoundTrip(req)

			wantRetries := maxRetries
			if e.retries < wantRetries {
				wantRetries = e.retries
			}

			if gotRetries != wantRetries {
				t.Errorf("Unexpected number of retries. Want %d, got %d", wantRetries, gotRetries)
			}

			if e.wantErr {
				if err == nil {
					t.Errorf("Expected error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				gotBody, _ := ioutil.ReadAll(resp.Body)
				if string(gotBody) != wantBody {
					t.Errorf("Unexpected response. Want %q, got %q", wantBody, gotBody)
				}
			}
		})
	}
}

func TestStatusFilterRoundTripper(t *testing.T) {
	testServer := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
	}

	goodRT := http.DefaultTransport
	errorRT := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("some error")
	})

	filtered := []int{501, 502}

	examples := []struct {
		label     string
		transport http.RoundTripper
		status    int
		err       error
	}{
		{
			label:     "filtered status",
			transport: goodRT,
			status:    502,
			err:       errors.New("Filtering 502"),
		},
		{
			label:     "unfiltered status",
			transport: goodRT,
			status:    503,
			err:       nil,
		},
		{
			label:     "transport error",
			transport: errorRT,
			status:    200,
			err:       errors.New("some error"),
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			ts := testServer(e.status)
			defer ts.Close()

			rt := newStatusFilterRoundTripper(e.transport, filtered...)

			req := httptest.NewRequest("", ts.URL, nil)
			resp, err := rt.RoundTrip(req)

			if e.err != nil {
				if err.Error() != e.err.Error() {
					t.Errorf("Unexpected error. Want %v, got %v", e.err, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error %v", err)
				}
				if resp.StatusCode != e.status {
					t.Errorf("Unexpected response status. Want %d, got %d", e.status, resp.StatusCode)
				}
			}
		})
	}
}
