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

package testing

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
)

// FakeResponse is a response given by the FakeRoundTripper
type FakeResponse struct {
	Err  error
	Code int
	Body string
}

// FakeRoundTripper is a roundtripper emulator useful in testing
type FakeRoundTripper struct {
	// Return an error if host header does not match this
	ExpectHost string

	// LockerCh blocks responses being sent until a struct is written to the channel
	LockerCh chan struct{}

	// Responses to probe requests are popeed from this list until it is size 1 then
	// that response is returned indefinitely
	ProbeResponses []FakeResponse

	// Response to non-probe requests
	RequestResponse *FakeResponse
	responseMux     sync.Mutex
}

func defaultProbeResponse() *FakeResponse {
	return &FakeResponse{
		Err:  nil,
		Code: http.StatusOK,
		Body: queue.Name,
	}
}

func defaultRequestResponse() *FakeResponse {
	return &FakeResponse{
		Err:  nil,
		Code: http.StatusOK,
		Body: "default response",
	}
}

func response(fr *FakeResponse) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	recorder.WriteHeader(fr.Code)
	recorder.WriteString(fr.Body)
	return recorder.Result(), nil
}

func (rt *FakeRoundTripper) popResponse() *FakeResponse {
	rt.responseMux.Lock()
	defer rt.responseMux.Unlock()

	responses := rt.ProbeResponses
	if responses == nil || len(responses) == 0 {
		return defaultProbeResponse()
	}
	resp := &responses[0]
	if len(responses) > 1 {
		rt.ProbeResponses = responses[1:]
	}
	return resp
}

// RT is a RoundTripperFunc
func (rt *FakeRoundTripper) RT(req *http.Request) (*http.Response, error) {
	if req.Header.Get(network.ProbeHeaderName) != "" {
		resp := rt.popResponse()
		if resp.Err != nil {
			return nil, resp.Err
		}

		// Make sure the probe is attributed with correct header.
		if req.Header.Get(network.ProbeHeaderName) != queue.Name {
			return response(&FakeResponse{
				Code: http.StatusBadRequest,
				Body: "probe sent to a wrong system",
			})
		}
		return response(resp)
	}
	resp := rt.RequestResponse
	if resp == nil {
		resp = defaultRequestResponse()
	}

	if resp.Err != nil {
		return nil, resp.Err
	}

	// verify that the request has the required rewritten host header.
	if got, want := req.Host, ""; got != want {
		return nil, fmt.Errorf("the req.Host has not been cleared out, was: %q", got)
	}
	if got, want := req.Header.Get("Host"), ""; got != want {
		return nil, fmt.Errorf("the Host header has not been cleared out, was: %q", got)
	}

	if rt.ExpectHost != "" {
		if got, want := req.Header.Get(network.OriginalHostHeader), rt.ExpectHost; got != want {
			return nil, fmt.Errorf("the %s header = %q, want: %q", network.OriginalHostHeader, got, want)
		}
	}

	if rt.LockerCh != nil {
		rt.LockerCh <- struct{}{}
	}

	return response(resp)
}
