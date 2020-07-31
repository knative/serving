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
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	network "knative.dev/networking/pkg"
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
	// TODO: Use Prometheus lib to generate the following text instead of using text format directly.
	testAverageConcurrencyContext = fmt.Sprintf(`# HELP queue_average_concurrent_requests Number of requests currently being handled by this pod
# TYPE queue_average_concurrent_requests gauge
queue_average_concurrent_requests{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} %f
`, queueAverageConcurrentRequests)
	testQPSContext = fmt.Sprintf(`# HELP queue_requests_per_second Number of requests received since last Stat
# TYPE queue_requests_per_second gauge
queue_requests_per_second{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} %d
`, queueRequestsPerSecond)
	testAverageProxiedConcurrenyContext = fmt.Sprintf(`# HELP queue_average_proxied_concurrent_requests Number of proxied requests currently being handled by this pod
# TYPE queue_average_proxied_concurrent_requests gauge
queue_average_proxied_concurrent_requests{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} %f
`, queueAverageProxiedConcurrentRequests)
	testProxiedQPSContext = fmt.Sprintf(`# HELP queue_proxied_operations_per_second Number of proxied requests received since last Stat
# TYPE queue_proxied_operations_per_second gauge
queue_proxied_operations_per_second{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} %d
`, queueProxiedOperationsPerSecond)
	testUptimeContext = fmt.Sprintf(`# HELP process_uptime The number of seconds that the process has been up
# TYPE process_uptime gauge
process_uptime{destination_configuration="s1",destination_namespace="default",destination_pod="s1-tdgpn-deployment-86f6459cf8-mc9mw",destination_revision="s1-tdgpn"} %f
`, processUptime)
	testFullContext = testAverageConcurrencyContext + testQPSContext + testAverageProxiedConcurrenyContext + testProxiedQPSContext + testUptimeContext

	stat = Stat{
		PodName:                          podName,
		AverageConcurrentRequests:        queueAverageConcurrentRequests,
		AverageProxiedConcurrentRequests: queueAverageProxiedConcurrentRequests,
		RequestCount:                     queueRequestsPerSecond,
		ProxiedRequestCount:              queueProxiedOperationsPerSecond,
		ProcessUptime:                    processUptime,
	}
)

func TestNewHTTPScrapeClientErrorCases(t *testing.T) {
	testCases := []struct {
		name        string
		client      *http.Client
		expectedErr string
	}{{
		name:        "Empty HTTP client",
		expectedErr: "HTTP client must not be nil",
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newHTTPScrapeClient(test.client); err != nil {
				got := err.Error()
				want := test.expectedErr
				if got != want {
					t.Errorf("Got error message: %v. Want: %v", got, want)
				}
			} else {
				t.Error("Expected error from newHTTPScrapeClient, got nil")
			}
		})
	}
}

func TestHTTPScrapeClientScrapeHappyCaseWithOptionals(t *testing.T) {
	for _, hClient := range []*http.Client{
		newTestHTTPClient(makeResponse(http.StatusOK, testFullContext), nil),
		newTestHTTPClient(makeProtoResponse(http.StatusOK, stat, network.ProtoAcceptContent), nil),
	} {
		sClient, err := newHTTPScrapeClient(hClient)
		if err != nil {
			t.Fatalf("newHTTPScrapeClient = %v, want no error", err)
		}
		got, err := sClient.Scrape(testURL)
		if err != nil {
			t.Fatalf("Scrape = %v, want no error", err)
		}
		if !cmp.Equal(got, stat) {
			t.Errorf("Scraped stat mismatch; diff(-want,+got):\n%s", cmp.Diff(stat, got))
		}
	}
}

func TestHTTPScrapeClientScrapeErrorCases(t *testing.T) {
	testCases := []struct {
		name            string
		responseCode    int
		responseErr     error
		responseContext string
		expectedErr     string
	}{{
		name:         "Non 200 return code",
		responseCode: http.StatusForbidden,
		expectedErr:  fmt.Sprintf(`GET request for URL %q returned HTTP status 403`, testURL),
	}, {
		name:         "Error got when sending request",
		responseCode: http.StatusOK,
		responseErr:  errors.New("upstream closed"),
		expectedErr:  "upstream closed",
	}, {
		name:            "Bad response context format",
		responseCode:    http.StatusOK,
		responseContext: "bad context",
		expectedErr:     "reading text format failed: text format parsing error in line 1: unexpected end of input stream",
	}, {
		name:            "Missing average concurrency",
		responseCode:    http.StatusOK,
		responseContext: testQPSContext + testAverageProxiedConcurrenyContext + testProxiedQPSContext,
		expectedErr:     "could not find value for queue_average_concurrent_requests in response",
	}, {
		name:            "Missing QPS",
		responseCode:    http.StatusOK,
		responseContext: testAverageConcurrencyContext + testAverageProxiedConcurrenyContext + testProxiedQPSContext,
		expectedErr:     "could not find value for queue_requests_per_second in response",
	}, {
		name:            "Missing average proxied concurrency",
		responseCode:    http.StatusOK,
		responseContext: testAverageConcurrencyContext + testQPSContext + testProxiedQPSContext,
		expectedErr:     "could not find value for queue_average_proxied_concurrent_requests in response",
	}, {
		name:            "Missing proxied QPS",
		responseCode:    http.StatusOK,
		responseContext: testAverageConcurrencyContext + testQPSContext + testAverageProxiedConcurrenyContext,
		expectedErr:     "could not find value for queue_proxied_operations_per_second in response",
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			hClient := newTestHTTPClient(makeResponse(test.responseCode, test.responseContext), test.responseErr)
			sClient, err := newHTTPScrapeClient(hClient)
			if err != nil {
				t.Errorf("newHTTPScrapeClient=%v, want no error", err)
			}
			if _, err := sClient.Scrape(testURL); err != nil {
				if !strings.Contains(err.Error(), test.expectedErr) {
					t.Errorf("Got error message: %q, want to contain: %q", err.Error(), test.expectedErr)
				}
			} else {
				t.Error("Expected error from newServiceScraperWithClient, got nil")
			}
		})
	}
}

func TestHTTPScrapeClientScrapeProtoErrorCases(t *testing.T) {
	testCases := []struct {
		name         string
		responseCode int
		responseErr  error
		stat         Stat
		expectedErr  string
	}{{
		name:         "Non 200 return code",
		responseCode: http.StatusForbidden,
		expectedErr:  fmt.Sprintf("GET request for URL %q returned HTTP status 403", testURL),
	}, {
		name:         "Error got when sending request",
		responseCode: http.StatusOK,
		responseErr:  errors.New("upstream closed"),
		expectedErr:  "upstream closed",
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			hClient := newTestHTTPClient(makeProtoResponse(test.responseCode, test.stat, network.ProtoAcceptContent), test.responseErr)
			sClient, err := newHTTPScrapeClient(hClient)
			if err != nil {
				t.Fatalf("newHTTPScrapeClient=%v, want no error", err)
			}
			_, err = sClient.Scrape(testURL)
			if err == nil {
				t.Fatal("Got no error")
			}
			if !strings.Contains(err.Error(), test.expectedErr) {
				t.Errorf("Error = %q, want to contain: %q", err.Error(), test.expectedErr)
			}
		})
	}
}

func makeResponse(statusCode int, context string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       ioutil.NopCloser(bytes.NewBufferString(context)),
	}
}

func makeProtoResponse(statusCode int, stat Stat, contentType string) *http.Response {
	buffer, _ := stat.Marshal()
	res := &http.Response{
		StatusCode: statusCode,
		Body:       ioutil.NopCloser(bytes.NewBuffer(buffer)),
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
	}{
		{
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
		},
	}
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
