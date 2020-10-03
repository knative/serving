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

package autotls

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/serving/test/types"
)

type requestOption func(*http.Request)
type responseExpectation func(response *http.Response) error

func runtimeRequest(t *testing.T, client *http.Client, url string, opts ...requestOption) *types.RuntimeInfo {
	t.Helper()
	return runtimeRequestWithExpectations(t, client, url,
		[]responseExpectation{statusCodeExpectation(sets.NewInt(http.StatusOK))},
		false,
		opts...)
}

// runtimeRequestWithExpectations attempts to make a request to url and return runtime information.
// If connection is successful only then it will validate all response expectations.
// If allowDialError is set to true then function will not fail if connection is a dial error.
func runtimeRequestWithExpectations(t *testing.T, client *http.Client, url string,
	responseExpectations []responseExpectation,
	allowDialError bool,
	opts ...requestOption) *types.RuntimeInfo {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Error("Error creating Request:", err)
		return nil
	}

	for _, opt := range opts {
		opt(req)
	}

	resp, err := client.Do(req)

	if err != nil {
		if !allowDialError || !isDialError(err) {
			t.Error("Error making GET request:", err)
		}
		return nil
	}

	defer resp.Body.Close()

	for _, e := range responseExpectations {
		if err := e(resp); err != nil {
			t.Error("Error meeting response expectations:", err)
			dumpResponse(t, resp)
			return nil
		}
	}

	if resp.StatusCode == http.StatusOK {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Error("Unable to read response body:", err)
			dumpResponse(t, resp)
			return nil
		}
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Error("Unable to parse runtime image's response payload:", err)
			return nil
		}
		return ri
	}
	return nil
}

func dumpResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	b, err := httputil.DumpResponse(resp, true)
	if err != nil {
		t.Error("Error dumping response:", err)
	}
	t.Log(string(b))
}

func statusCodeExpectation(statusCodes sets.Int) responseExpectation {
	return func(response *http.Response) error {
		if !statusCodes.Has(response.StatusCode) {
			return fmt.Errorf("got unexpected status: %d, expected %v", response.StatusCode, statusCodes)
		}
		return nil
	}
}

func isDialError(err error) bool {
	if err, ok := err.(*url.Error); ok {
		err, ok := err.Err.(*net.OpError)
		return ok && err.Op == "dial"
	}
	return false
}
