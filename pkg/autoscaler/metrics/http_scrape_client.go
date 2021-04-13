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

package metrics

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	network "knative.dev/networking/pkg"
)

var errUnsupportedMetricType = errors.New("unsupported metric type")

// meshErrorStatusCode is the HTTP status code returned when mesh is enabled
// and direct pod access is attempted. This is used to distinguish potential
// mesh errors from unrelated errors.
const meshErrorStatusCode = http.StatusServiceUnavailable

type httpScrapeClient struct {
	httpClient *http.Client
}

type errorWithStatusCode struct {
	error
	statusCode int
}

var pool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func newHTTPScrapeClient(httpClient *http.Client) *httpScrapeClient {
	return &httpScrapeClient{
		httpClient: httpClient,
	}
}

func (c *httpScrapeClient) Do(req *http.Request) (Stat, error) {
	req.Header.Add("Accept", network.ProtoAcceptContent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return emptyStat, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return emptyStat, errorWithStatusCode{
			error:      fmt.Errorf("GET request for URL %q returned HTTP status %v", req.URL.String(), resp.StatusCode),
			statusCode: resp.StatusCode,
		}
	}
	if resp.Header.Get("Content-Type") != network.ProtoAcceptContent {
		return emptyStat, errUnsupportedMetricType
	}
	return statFromProto(resp.Body)
}

// isMeshError returns true if the error may have been caused by attempting to
// access a pod directly when mesh is enabled.
// TODO(julz) consider other tests, e.g. looking for upstream header in response.
func isMeshError(err error) bool {
	var errWithStatus errorWithStatusCode
	if errors.As(err, &errWithStatus) {
		return errWithStatus.statusCode == meshErrorStatusCode
	}

	return false
}

func statFromProto(body io.Reader) (Stat, error) {
	var stat Stat
	b := pool.Get().(*bytes.Buffer)
	b.Reset()
	defer pool.Put(b)
	_, err := b.ReadFrom(body)
	if err != nil {
		return emptyStat, fmt.Errorf("reading body failed: %w", err)
	}
	err = stat.Unmarshal(b.Bytes())
	if err != nil {
		return emptyStat, fmt.Errorf("unmarshalling failed: %w", err)
	}
	return stat, nil
}
