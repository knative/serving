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

	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/serving"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
)

const (
	testRevision  = "test-revision"
	testService   = "test-revision-service"
	testNamespace = "test-namespace"
	testPodName   = "test-revision-1234"
	testKPAKey    = "test-namespace/test-revision"
	testURL       = "http://test-revision-service.test-namespace:9090/metrics"

	testAverageConcurrenyContext = `# HELP queue_average_concurrent_requests Number of requests currently being handled by this pod
# TYPE queue_average_concurrent_requests gauge
queue_average_concurrent_requests{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} 2.0
`
	testQPSContext = `# HELP queue_operations_per_second Number of requests received since last Stat
# TYPE queue_operations_per_second gauge
queue_operations_per_second{destination_namespace="test-namespace",destination_revision="test-revision",destination_pod="test-revision-1234"} 1
`
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
	metric := getTestMetric()
	invalidMetric := getTestMetric()
	invalidMetric.Labels = map[string]string{}
	dynConfig := &DynamicConfig{}
	client := newTestClient(nil, nil)
	informer := kubeInformer.Core().V1().Endpoints()
	testCases := []struct {
		name        string
		metric      *Metric
		dynConfig   *DynamicConfig
		client      *http.Client
		informer    corev1informers.EndpointsInformer
		expectedErr string
	}{{
		name:        "Empty Metric",
		dynConfig:   dynConfig,
		client:      client,
		informer:    informer,
		expectedErr: "metric must not be nil",
	}, {
		name:        "Missing revision label in Metric",
		metric:      &invalidMetric,
		dynConfig:   dynConfig,
		client:      client,
		informer:    informer,
		expectedErr: "no Revision label found for Metric test-revision",
	}, {
		name:        "Empty DynamicConfig",
		metric:      &metric,
		client:      client,
		informer:    informer,
		expectedErr: "dynamic config must not be nil",
	}, {
		name:        "Empty HTTP client",
		metric:      &metric,
		dynConfig:   dynConfig,
		informer:    informer,
		expectedErr: "HTTP client must not be nil",
	}, {
		name:        "Empty informer",
		metric:      &metric,
		dynConfig:   dynConfig,
		client:      client,
		expectedErr: "endpoints informer must not be nil",
	}}

	for _, test := range testCases {
		if _, err := newServiceScraperWithClient(test.metric, test.dynConfig, test.informer, test.client); err != nil {
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
	client := newTestClient(getHTTPResponse(http.StatusOK, testAverageConcurrenyContext+testQPSContext), nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}
	stat, err := scraper.scrapeViaURL()
	if err != nil {
		t.Errorf("scrapeViaURL=%v, want no error", err)
	}
	if stat.AverageConcurrentRequests != 2.0 {
		t.Errorf("stat.AverageConcurrentRequests=%v, want 2.0", stat.AverageConcurrentRequests)
	}
	if stat.RequestCount != 1 {
		t.Errorf("stat.RequestCount=%v, want 1", stat.RequestCount)
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
		expectedErr:  "GET request for URL \"http://test-revision-service.test-namespace:9090/metrics\" returned HTTP status 403",
	}, {
		name:         "Error got when sending request",
		responseCode: http.StatusOK,
		responseErr:  errors.New("upstream closed"),
		expectedErr:  "Get http://test-revision-service.test-namespace:9090/metrics: upstream closed",
	}, {
		name:            "Bad response context format",
		responseCode:    http.StatusOK,
		responseContext: "bad context",
		expectedErr:     "Reading text format failed: text format parsing error in line 1: unexpected end of input stream",
	}, {
		name:            "Missing average concurrency",
		responseCode:    http.StatusOK,
		responseContext: testQPSContext,
		expectedErr:     "Could not find value for queue_average_concurrent_requests in response",
	}, {
		name:            "Missing QPS",
		responseCode:    http.StatusOK,
		responseContext: testAverageConcurrenyContext,
		expectedErr:     "Could not find value for queue_operations_per_second in response",
	}}

	for _, test := range testCases {
		client := newTestClient(getHTTPResponse(test.responseCode, test.responseContext), test.responseErr)
		scraper, err := serviceScraperForTest(client)
		if err != nil {
			t.Errorf("newServiceScraperWithClient=%v, want no error", err)
		}
		if _, err := scraper.scrapeViaURL(); err != nil {
			if err.Error() != test.expectedErr {
				t.Errorf("Got error message: %v. Want: %v", err.Error(), test.expectedErr)
			}
		} else {
			t.Errorf("Expected error from newServiceScraperWithClient, got nil")
		}
	}
}

func TestScrape_HappyCase(t *testing.T) {
	client := newTestClient(getHTTPResponse(http.StatusOK, testAverageConcurrenyContext+testQPSContext), nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Make an Endpoints with 2 pods.
	createEndpoints(addIps(makeEndpoints(), 2))
	// Scrape will set a timestamp bigger than this.
	now := time.Now()
	statsCh := make(chan *StatMessage, 1)
	defer close(statsCh)
	scraper.Scrape(TestContextWithLogger(t), statsCh)

	got := <-statsCh
	if got.Key != testKPAKey {
		t.Errorf("StatMessage.Key=%v, want %v", got.Key, testKPAKey)
	}
	if got.Stat.Time.Before(now) {
		t.Errorf("StatMessage.Stat.Time=%v, want bigger than %v", got.Stat.Time, now)
	}
	if got.Stat.PodName != scraperPodName {
		t.Errorf("StatMessage.Stat.PodName=%v, want %v", got.Stat.PodName, scraperPodName)
	}
	// 2 pods times 2.0
	if got.Stat.AverageConcurrentRequests != 4.0 {
		t.Errorf("StatMessage.Stat.AverageConcurrentRequests=%v, want %v",
			got.Stat.AverageConcurrentRequests, 4.0)
	}
	// 2 pods times 1
	if got.Stat.RequestCount != 2 {
		t.Errorf("StatMessage.Stat.RequestCount=%v, want %v", got.Stat.RequestCount, 2)
	}
}

func TestScrape_DoNotScrapeIfNoPodsFound(t *testing.T) {
	client := newTestClient(getHTTPResponse(200, testAverageConcurrenyContext+testQPSContext), nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Override the Endpoints with 0 pods.
	createEndpoints(addIps(makeEndpoints(), 0))

	statsCh := make(chan *StatMessage, 1)
	defer close(statsCh)
	scraper.Scrape(TestContextWithLogger(t), statsCh)

	select {
	case <-statsCh:
		t.Error("Received unexpected StatMessage.")
	case <-time.After(300 * time.Millisecond):
		// We got nothing!
	}
}

func serviceScraperForTest(httpClient *http.Client) (*ServiceScraper, error) {
	metric := getTestMetric()
	dynConfig := &DynamicConfig{}
	return newServiceScraperWithClient(&metric, dynConfig, kubeInformer.Core().V1().Endpoints(), httpClient)
}

func getTestMetric() Metric {
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
