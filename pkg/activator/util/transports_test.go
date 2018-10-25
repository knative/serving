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
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"testing"

	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/activator"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestHTTPRoundTripper(t *testing.T) {
	wants := map[string]bool{}
	frt := func(key string) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			wants[key] = true

			return nil, nil
		})
	}

	rt := NewHTTPTransport(frt("v1"), frt("v2"))

	examples := []struct {
		label      string
		protoMajor int
		want       string
	}{
		{
			label:      "use default transport for HTTP1",
			protoMajor: 1,
			want:       "v1",
		},
		{
			label:      "use h2c transport for HTTP2",
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
	req := &http.Request{Header: http.Header{}}

	resp := func(status int) *http.Response {
		return &http.Response{StatusCode: status, Body: &spyReadCloser{}}
	}

	someErr := errors.New("some error")
	conditions := []RetryCond{
		RetryStatus(http.StatusInternalServerError),
		RetryStatus(http.StatusBadRequest),
	}

	examples := []struct {
		label          string
		resp           *http.Response
		err            error
		cond           []RetryCond
		wantAttempts   int
		wantBodyClosed bool
	}{
		{
			label:          "no retry",
			resp:           resp(http.StatusOK),
			err:            nil,
			cond:           conditions,
			wantAttempts:   1,
			wantBodyClosed: false,
		},
		{
			label:          "no conditions",
			resp:           resp(http.StatusInternalServerError),
			err:            nil,
			cond:           []RetryCond{},
			wantAttempts:   1,
			wantBodyClosed: false,
		},
		{
			label:          "retry on error",
			resp:           nil,
			err:            someErr,
			cond:           conditions,
			wantAttempts:   2,
			wantBodyClosed: false,
		},
		{
			label:          "retry on condition 1",
			resp:           resp(http.StatusInternalServerError),
			err:            nil,
			cond:           conditions,
			wantAttempts:   2,
			wantBodyClosed: true,
		},
		{
			label:          "retry on condition 2",
			resp:           resp(http.StatusBadRequest),
			err:            nil,
			cond:           conditions,
			wantAttempts:   2,
			wantBodyClosed: true,
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			allRequestsGotRetryHeader := true
			transport := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get(activator.RequestCountHTTPHeader) == "" {
					allRequestsGotRetryHeader = false
				}

				return e.resp, e.err
			})

			rt := NewRetryRoundTripper(
				transport,
				TestLogger(t),
				wait.Backoff{Steps: 2},
				e.cond...,
			)
			resp, err := rt.RoundTrip(req)

			if resp != e.resp {
				t.Errorf("Unexpected response. Want %v, got %v", e.resp, resp)
			}

			if err != e.err {
				t.Errorf("Unexpected error. Want %v, got %v", e.err, err)
			}

			if e.wantBodyClosed && !e.resp.Body.(*spyReadCloser).Closed {
				t.Errorf("Expected response body to be closed.")
			}

			if !allRequestsGotRetryHeader {
				t.Errorf("Not all retry requests had the retry header set.")
			}

			if resp != nil {
				if got, want := resp.Header.Get(activator.ResponseCountHTTPHeader), strconv.Itoa(e.wantAttempts); got != want {
					t.Errorf("Expected retry header not the same got: %q want: %q", got, want)
				}
			}
		})
	}
}

func TestRetryRoundTripperRewind(t *testing.T) {
	bodyContent := "request body"

	readingCondition := func(res *http.Response) bool {
		body, _ := ioutil.ReadAll(res.Body)
		res.Body.Close()

		responseBodyContent := string(body[:])

		if responseBodyContent != bodyContent {
			t.Errorf("Body was not readable multiple times. Was %s", responseBodyContent)
		}
		return false
	}

	// try to read the body twice
	conditions := []RetryCond{
		readingCondition,
		readingCondition,
	}

	transport := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: r.Body}, nil
	})

	rt := NewRetryRoundTripper(
		transport,
		TestLogger(t),
		wait.Backoff{Steps: 1},
		conditions...,
	)

	spy := &spyReadCloser{Reader: strings.NewReader(bodyContent)}
	req, _ := http.NewRequest("POST", "http://test.domain", spy)

	rt.RoundTrip(req)

	if spy.ReadAfterClose {
		t.Fatal("The retry round tripper read the request body more than once")
	}
}

func TestRetryRoundTripperNilBody(t *testing.T) {
	rt := NewRetryRoundTripper(
		http.DefaultTransport,
		TestLogger(t),
		wait.Backoff{Steps: 1},
		RetryStatus(http.StatusInternalServerError),
	)

	req, _ := http.NewRequest("GET", "http://knative.dev/test/", nil)

	rt.RoundTrip(req)
}
