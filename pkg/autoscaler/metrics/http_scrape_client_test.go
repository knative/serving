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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	netheader "knative.dev/networking/pkg/http/header"
)

const (
	queueAverageConcurrentRequests        = 3.0
	queueRequestsPerSecond                = 5
	queueAverageProxiedConcurrentRequests = 2.0
	queueProxiedOperationsPerSecond       = 4
	processUptime                         = 2937.12
	podName                               = "test-revision-1234"
)

var (
	testURL = "http://test-revision-zhudex.test-namespace:9090/metrics"

	stat = Stat{
		PodName:                          podName,
		AverageConcurrentRequests:        queueAverageConcurrentRequests,
		AverageProxiedConcurrentRequests: queueAverageProxiedConcurrentRequests,
		RequestCount:                     queueRequestsPerSecond,
		ProxiedRequestCount:              queueProxiedOperationsPerSecond,
		ProcessUptime:                    processUptime,
	}
)

func TestHTTPScrapeClientScrapeHappyCaseWithOptionals(t *testing.T) {
	hClient := newTestHTTPClient(makeProtoResponse(http.StatusOK, stat, netheader.ProtobufMIMEType), nil)
	sClient := newHTTPScrapeClient(hClient)
	req, err := http.NewRequest(http.MethodGet, testURL, nil)
	if err != nil {
		t.Fatalf("Failed to create a request: %v", err)
	}
	got, err := sClient.Do(req)
	if err != nil {
		t.Fatalf("Scrape = %v, want no error", err)
	}
	if !cmp.Equal(got, stat) {
		t.Errorf("Scraped stat mismatch; diff(-want,+got):\n%s", cmp.Diff(stat, got))
	}
}

func TestHTTPScrapeClientScrapeProtoErrorCases(t *testing.T) {
	testCases := []struct {
		name            string
		responseCode    int
		responseType    string
		responseErr     error
		stat            Stat
		expectedErr     string
		expectedMeshErr bool
	}{{
		name:         "Non 200 return code",
		responseCode: http.StatusForbidden,
		expectedErr:  fmt.Sprintf("GET request for URL %q returned HTTP status 403", testURL),
	}, {
		name:            "503 return code",
		responseCode:    http.StatusServiceUnavailable,
		expectedErr:     fmt.Sprintf("GET request for URL %q returned HTTP status 503", testURL),
		expectedMeshErr: true,
	}, {
		name:         "Error got when sending request",
		responseCode: http.StatusOK,
		responseErr:  errors.New("upstream closed"),
		expectedErr:  "upstream closed",
	}, {
		name:         "Wrong Content-Type",
		responseCode: http.StatusOK,
		responseType: "text/html",
		expectedErr:  errUnsupportedMetricType.Error(),
	}, {
		name:         "LongStat",
		responseCode: http.StatusOK,
		responseType: "application/protobuf",
		stat: Stat{
			// We don't expect PodName to be 600 characters long
			PodName:                          strings.Repeat("a123456789", 60),
			AverageConcurrentRequests:        1.1,
			AverageProxiedConcurrentRequests: 1.1,
			RequestCount:                     33.2,
			ProxiedRequestCount:              33.2,
			ProcessUptime:                    12345.678,
			Timestamp:                        1697431278,
		},
		expectedErr: "unmarshalling failed: unexpected EOF",
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			hClient := newTestHTTPClient(makeProtoResponse(test.responseCode, test.stat, test.responseType), test.responseErr)
			sClient := newHTTPScrapeClient(hClient)
			req, err := http.NewRequest(http.MethodGet, testURL, nil)
			if err != nil {
				t.Fatalf("Failed to create a request: %v", err)
			}
			_, err = sClient.Do(req)
			if err == nil {
				t.Fatal("Got no error")
			}
			if !strings.Contains(err.Error(), test.expectedErr) {
				t.Errorf("Error = %q, want to contain: %q", err.Error(), test.expectedErr)
			}
			if got := isPotentialMeshError(err); got != test.expectedMeshErr {
				t.Errorf("isMeshError(err) = %v, expected %v", got, test.expectedMeshErr)
			}
		})
	}
}

func makeProtoResponse(statusCode int, stat Stat, contentType string) *http.Response {
	buffer, _ := stat.Marshal()
	res := &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBuffer(buffer)),
	}
	res.Header = http.Header{}
	res.Header.Set("Content-Type", contentType)
	return res
}

type fakeRoundTripper struct {
	response      *http.Response
	responseError error
}

func (frt fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return frt.response, frt.responseError
}

func newTestHTTPClient(response *http.Response, err error) *http.Client {
	return &http.Client{
		Transport: fakeRoundTripper{
			response:      response,
			responseError: err,
		},
	}
}

func BenchmarkUnmarshallingProtoData(b *testing.B) {
	stat := Stat{
		ProcessUptime:                    12.2,
		AverageConcurrentRequests:        2.3,
		AverageProxiedConcurrentRequests: 100.2,
		RequestCount:                     122,
		ProxiedRequestCount:              122,
	}

	benchmarks := []struct {
		name    string
		podName string
	}{{
		name:    "BenchmarkStatWithEmptyPodName",
		podName: "",
	}, {
		name:    "BenchmarkStatWithSmallPodName",
		podName: "a-lzrjc-deployment-85d4b7d859-gspjs",
	}, {
		name:    "BenchmarkStatWithMediumPodName",
		podName: "a-lzrjc-deployment-85d4b7d859-gspjs" + strings.Repeat("p", 50),
	}, {
		name:    "BenchmarkStatWithLargePodName",
		podName: strings.Repeat("p", 253),
	}}
	for _, bm := range benchmarks {
		stat.PodName = bm.podName
		bodyBytes, err := stat.Marshal()
		if err != nil {
			b.Fatal(err)
		}
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err = statFromProto(bytes.NewReader(bodyBytes))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
