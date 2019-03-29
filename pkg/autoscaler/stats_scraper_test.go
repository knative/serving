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

package autoscaler

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	testRevision  = "test-revision"
	testService   = "test-revision-metrics"
	testNamespace = "test-namespace"
	testKPAKey    = "test-namespace/test-revision"
	testURL       = "http://test-revision-metrics.test-namespace:9090/metrics"

	testAverageConcurrencyContext = `# HELP queue_average_concurrent_requests Number of requests currently being handled by this pod
# TYPE queue_average_concurrent_requests gauge
queue_average_concurrent_requests{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} 3.0
`
	testQPSContext = `# HELP queue_operations_per_second Number of requests received since last Stat
# TYPE queue_operations_per_second gauge
queue_operations_per_second{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} 5
`
	testAverageProxiedConcurrenyContext = `# HELP queue_average_proxied_concurrent_requests Number of proxied requests currently being handled by this pod
# TYPE queue_average_proxied_concurrent_requests gauge
queue_average_proxied_concurrent_requests{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} 2.0
`
	testProxiedQPSContext = `# HELP queue_proxied_operations_per_second Number of proxied requests received since last Stat
# TYPE queue_proxied_operations_per_second gauge
queue_proxied_operations_per_second{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} 4
`
	testFullContext = testAverageConcurrencyContext + testQPSContext + testAverageProxiedConcurrenyContext + testProxiedQPSContext
)

func TestNewServiceScraperWithClient_HappyCase(t *testing.T) {
	client := newTestClient(nil, nil)
	if scraper, err := serviceScraperForTest(client); err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	} else {
		if scraper.url != testURL {
			t.Errorf("scraper.url=%v, want %v", scraper.url, testURL)
		}
		if scraper.metricKey != testKPAKey {
			t.Errorf("scraper.metricKey=%v, want %v", scraper.metricKey, testKPAKey)
		}
	}
}

func TestNewServiceScraperWithClient_ErrorCases(t *testing.T) {
	metric := testMetric()
	invalidMetric := testMetric()
	invalidMetric.Labels = map[string]string{}
	client := newTestClient(nil, nil)
	lister := kubeInformer.Core().V1().Endpoints().Lister()
	testCases := []struct {
		name        string
		metric      *Metric
		client      *http.Client
		lister      corev1listers.EndpointsLister
		expectedErr string
	}{{
		name:        "Empty Metric",
		client:      client,
		lister:      lister,
		expectedErr: "metric must not be nil",
	}, {
		name:        "Missing revision label in Metric",
		metric:      &invalidMetric,
		client:      client,
		lister:      lister,
		expectedErr: "no Revision label found for Metric test-revision",
	}, {
		name:        "Empty HTTP client",
		metric:      &metric,
		lister:      lister,
		expectedErr: "HTTP client must not be nil",
	}, {
		name:        "Empty lister",
		metric:      &metric,
		client:      client,
		expectedErr: "endpoints lister must not be nil",
	}}

	for _, test := range testCases {
		if _, err := newServiceScraperWithClient(test.metric, test.lister, test.client); err != nil {
			got := err.Error()
			want := test.expectedErr
			if got != want {
				t.Errorf("Got error message: %v. Want: %v", got, want)
			}
		} else {
			t.Errorf("Expected error from CreateNewServiceScraper, got nil")
		}
	}
}

func TestScrapeViaURL_HappyCase(t *testing.T) {
	client := newTestClient(getHTTPResponse(http.StatusOK, testFullContext), nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}
	stat, err := scraper.scrapeViaURL()
	if err != nil {
		t.Errorf("scrapeViaURL=%v, want no error", err)
	}
	if stat.AverageConcurrentRequests != 3.0 {
		t.Errorf("stat.AverageConcurrentRequests=%v, want 3.0", stat.AverageConcurrentRequests)
	}
	if stat.RequestCount != 5 {
		t.Errorf("stat.RequestCount=%v, want 5", stat.RequestCount)
	}
	if stat.AverageProxiedConcurrentRequests != 2.0 {
		t.Errorf("stat.AverageProxiedConcurrency=%v, want 2.0", stat.AverageProxiedConcurrentRequests)
	}
	if stat.ProxiedRequestCount != 4 {
		t.Errorf("stat.ProxiedCount=%v, want 4", stat.ProxiedRequestCount)
	}
}

func TestScrapeViaURL_ErrorCases(t *testing.T) {
	testCases := []struct {
		name            string
		responseCode    int
		responseErr     error
		responseContext string
		expectedErr     string
	}{{
		name:         "Non 200 return code",
		responseCode: http.StatusForbidden,
		expectedErr:  `GET request for URL "http://test-revision-metrics.test-namespace:9090/metrics" returned HTTP status 403`,
	}, {
		name:         "Error got when sending request",
		responseCode: http.StatusOK,
		responseErr:  errors.New("upstream closed"),
		expectedErr:  "Get http://test-revision-metrics.test-namespace:9090/metrics: upstream closed",
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
		expectedErr:     "could not find value for queue_operations_per_second in response",
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
		client := newTestClient(getHTTPResponse(test.responseCode, test.responseContext), test.responseErr)
		scraper, err := serviceScraperForTest(client)
		if err != nil {
			t.Errorf("newServiceScraperWithClient=%v, want no error", err)
		}
		if _, err := scraper.scrapeViaURL(); err != nil {
			if err.Error() != test.expectedErr {
				t.Errorf("Got error message: %q, want: %q", err.Error(), test.expectedErr)
			}
		} else {
			t.Errorf("Expected error from newServiceScraperWithClient, got nil")
		}
	}
}

func TestScrape_HappyCase(t *testing.T) {
	client := newTestClient(getHTTPResponse(http.StatusOK, testFullContext), nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Make an Endpoints with 2 pods.
	createEndpoints(addIps(makeEndpoints(), 2))
	// Scrape will set a timestamp bigger than this.
	now := time.Now()
	got, _ := scraper.Scrape()

	if got.Key != testKPAKey {
		t.Errorf("StatMessage.Key=%v, want %v", got.Key, testKPAKey)
	}
	if got.Stat.Time.Before(now) {
		t.Errorf("StatMessage.Stat.Time=%v, want bigger than %v", got.Stat.Time, now)
	}
	if got.Stat.PodName != scraperPodName {
		t.Errorf("StatMessage.Stat.PodName=%v, want %v", got.Stat.PodName, scraperPodName)
	}
	// 2 pods times 3.0
	if got.Stat.AverageConcurrentRequests != 6.0 {
		t.Errorf("StatMessage.Stat.AverageConcurrentRequests=%v, want %v",
			got.Stat.AverageConcurrentRequests, 6.0)
	}
	// 2 pods times 5
	if got.Stat.RequestCount != 10 {
		t.Errorf("StatMessage.Stat.RequestCount=%v, want %v", got.Stat.RequestCount, 10)
	}
	// 2 pods times 2.0
	if got.Stat.AverageProxiedConcurrentRequests != 4.0 {
		t.Errorf("StatMessage.Stat.AverageProxiedConcurrency=%v, want %v",
			got.Stat.AverageProxiedConcurrentRequests, 4.0)
	}
	// 2 pods times 4
	if got.Stat.ProxiedRequestCount != 8 {
		t.Errorf("StatMessage.Stat.ProxiedCount=%v, want %v", got.Stat.ProxiedRequestCount, 8)
	}
}

func TestScrape_DoNotScrapeIfNoPodsFound(t *testing.T) {
	client := newTestClient(getHTTPResponse(200, testFullContext), nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Override the Endpoints with 0 pods.
	createEndpoints(addIps(makeEndpoints(), 0))

	stat, err := scraper.Scrape()
	if stat != nil {
		t.Error("Received unexpected StatMessage.")
	}
}

func serviceScraperForTest(httpClient *http.Client) (*ServiceScraper, error) {
	metric := testMetric()
	return newServiceScraperWithClient(&metric, kubeInformer.Core().V1().Endpoints().Lister(), httpClient)
}

func testMetric() Metric {
	return Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
			Labels: map[string]string{
				serving.RevisionLabelKey: testRevision,
			},
		},
	}
}

func getHTTPResponse(statusCode int, context string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       ioutil.NopCloser(bytes.NewBufferString(context)),
	}
}

type fakeRoundTripper struct {
	response      *http.Response
	responseError error
}

func (frt fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return frt.response, frt.responseError
}

func newTestClient(response *http.Response, err error) *http.Client {
	return &http.Client{
		Transport: fakeRoundTripper{
			response:      response,
			responseError: err,
		},
	}
}
