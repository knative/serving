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

package autoscaler

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/serving"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testRevision  = "test-revision"
	testNamespace = "test-namespace"
	testKPAKey    = "test-namespace/test-revision"
	testURL       = "http://test-revision-service.test-namespace:9090/metrics"

	testAverageConcurrenyContext = `# HELP queue_average_concurrent_requests Number of requests currently being handled by this pod
# TYPE queue_average_concurrent_requests gauge
queue_average_concurrent_requests{destination_namespace="test-namespace",destination_revision="test-revision"} 2.0
`
	testQPSContext = `# HELP queue_operations_per_second Number of requests received since last Stat
# TYPE queue_operations_per_second gauge
queue_operations_per_second{destination_namespace="test-namespace",destination_revision="test-revision"} 1
`
)

func TestCreateServiceScraperWithClient_HappyCase(t *testing.T) {
	metric := getTestMetric()
	client := newTestClient(nil, nil)
	if scraper, err := createServiceScraperWithClient(&metric, TestLogger(t), client); err != nil {
		t.Errorf("createServiceScraperWithClient=%v, want no error", err)
	} else {
		if scraper.url != testURL {
			t.Errorf("scraper.url=%v, want %v", scraper.url, testURL)
		}
		if scraper.metricKey != testKPAKey {
			t.Errorf("scraper.metricKey=%v, want %v", scraper.metricKey, testKPAKey)
		}
	}
}

func TestCreateServiceScraperWithClient_ReturnErrorIfRevisionLabelIsMissing(t *testing.T) {
	metric := getTestMetric()
	metric.Labels = map[string]string{}
	client := newTestClient(nil, nil)
	if _, err := createServiceScraperWithClient(&metric, TestLogger(t), client); err != nil {
		got := err.Error()
		want := fmt.Sprintf("no Revision label found for Metric %s", testRevision)
		if got != want {
			t.Errorf("Got error message: %v. Want: %v", got, want)
		}
	} else {
		t.Errorf("Expected error from CreateNewServiceScraper, got nil")
	}
}

func TestScrapeViaURL_HappyCase(t *testing.T) {
	metric := getTestMetric()
	client := newTestClient(getHTTPResponse(200, testAverageConcurrenyContext+testQPSContext), nil)
	scraper, err := createServiceScraperWithClient(&metric, TestLogger(t), client)
	if err != nil {
		t.Errorf("createServiceScraperWithClient=%v, want no error", err)
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
	metric := getTestMetric()
	client := newTestClient(getHTTPResponse(200, testAverageConcurrenyContext+testQPSContext), nil)
	scraper, err := createServiceScraperWithClient(&metric, TestLogger(t), client)
	if err != nil {
		t.Errorf("createServiceScraperWithClient=%v, want no error", err)
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
