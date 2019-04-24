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
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	testRevision  = "test-revision"
	testService   = "test-revision-metrics"
	testNamespace = "test-namespace"
	testKPAKey    = "test-namespace/test-revision"
)

var (
	testStats = []*Stat{
		{
			AverageConcurrentRequests:        3.0,
			AverageProxiedConcurrentRequests: 2.0,
			RequestCount:                     5,
			ProxiedRequestCount:              4,
		}, {
			AverageConcurrentRequests:        5.0,
			AverageProxiedConcurrentRequests: 4.0,
			RequestCount:                     7,
			ProxiedRequestCount:              6,
		},
	}
)

func TestNewServiceScraperWithClient_HappyCase(t *testing.T) {
	client := newTestScrapeClient(testStats, []error{nil})
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
	client := newTestScrapeClient(testStats, []error{nil})
	lister := kubeInformer.Core().V1().Endpoints().Lister()
	testCases := []struct {
		name        string
		metric      *Metric
		client      scrapeClient
		lister      corev1listers.EndpointsLister
		expectedErr string
	}{{
		name:        "Empty Decider",
		client:      client,
		lister:      lister,
		expectedErr: "metric must not be nil",
	}, {
		name:        "Missing revision label in Decider",
		metric:      invalidMetric,
		client:      client,
		lister:      lister,
		expectedErr: "no Revision label found for Metric test-revision",
	}, {
		name:        "Empty scrape client",
		metric:      metric,
		lister:      lister,
		expectedErr: "scrape client must not be nil",
	}, {
		name:        "Empty lister",
		metric:      metric,
		client:      client,
		expectedErr: "endpoints lister must not be nil",
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newServiceScraperWithClient(test.metric, test.lister, test.client); err != nil {
				got := err.Error()
				want := test.expectedErr
				if got != want {
					t.Errorf("Got error message: %v. Want: %v", got, want)
				}
			} else {
				t.Errorf("Expected error from CreateNewServiceScraper, got nil")
			}
		})
	}
}

func TestScrape_HappyCase(t *testing.T) {
	client := newTestScrapeClient(testStats, []error{nil})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Make an Endpoints with 3 pods.
	createEndpoints(addIps(makeEndpoints(), 3))

	// Scrape will set a timestamp bigger than this.
	now := time.Now()
	got, err := scraper.Scrape()
	if err != nil {
		t.Fatalf("unexpected error from scraper.Scrape(): %v", err)
	}

	if got.Key != testKPAKey {
		t.Errorf("StatMessage.Key=%v, want %v", got.Key, testKPAKey)
	}
	if got.Stat.Time.Before(now) {
		t.Errorf("stat.Time=%v, want bigger than %v", got.Stat.Time, now)
	}
	if got.Stat.PodName != scraperPodName {
		t.Errorf("StatMessage.Stat.PodName=%v, want %v", got.Stat.PodName, scraperPodName)
	}
	// (3.0 + 5.0 + 3.0) / 3.0 * 3
	if got.Stat.AverageConcurrentRequests != 11.0 {
		t.Errorf("StatMessage.Stat.AverageConcurrentRequests=%v, want %v",
			got.Stat.AverageConcurrentRequests, 11.0)
	}
	// int32((5 + 7 + 5) / 3.0) * 3 = 15
	if got.Stat.RequestCount != 15 {
		t.Errorf("StatMessage.Stat.RequestCount=%v, want %v", got.Stat.RequestCount, 15)
	}
	// (2.0 + 4.0 + 2.0) / 3.0 * 3
	if got.Stat.AverageProxiedConcurrentRequests != 8.0 {
		t.Errorf("StatMessage.Stat.AverageProxiedConcurrentRequests=%v, want %v",
			got.Stat.AverageProxiedConcurrentRequests, 8.0)
	}
	// int32((4 + 6 + 4) / 3.0) * 3 = 12
	if got.Stat.ProxiedRequestCount != 12 {
		t.Errorf("StatMessage.Stat.ProxiedCount=%v, want %v", got.Stat.ProxiedRequestCount, 12)
	}
}

func TestScrape_ReportStat_ReachSampleSuccessRate(t *testing.T) {
	errTest := errors.New("test")
	client := newTestScrapeClient(testStats, []error{nil, nil, nil, nil, errTest})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Make an Endpoints with 5 pods. 80% success rate.
	createEndpoints(addIps(makeEndpoints(), 5))

	got, err := scraper.Scrape()
	if err != nil {
		t.Fatalf("unexpected error from scraper.Scrape(): %v", err)
	}
	// Only 4 samples.
	// (3.0 + 5.0 + 3.0 + 5.0) / 4.0 * 5
	if got.Stat.AverageConcurrentRequests != 20.0 {
		t.Errorf("StatMessage.Stat.AverageConcurrentRequests=%v, want %v",
			got.Stat.AverageConcurrentRequests, 20.0)
	}
}

func TestScrape_ReportError_LowerThanSampleSuccessRate(t *testing.T) {
	errTest := errors.New("test")
	client := newTestScrapeClient(testStats, []error{nil, errTest})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Make an Endpoints with 2 pods. 50% success rate.
	createEndpoints(addIps(makeEndpoints(), 2))

	_, err = scraper.Scrape()
	if err == nil {
		t.Errorf("Expected error from scraper.Scrape(), got nil")
	}
}

func TestScrape_PopulateErrorFromScrapeClient(t *testing.T) {
	errMsg := "test"
	client := newTestScrapeClient(testStats, []error{errors.New(errMsg)})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Make an Endpoints with 1 pod.
	createEndpoints(addIps(makeEndpoints(), 1))

	if _, err := scraper.Scrape(); err != nil {
		if got, want := err.Error(), errMsg; got != want {
			t.Errorf("Got error message: %v. Want: %v", got, want)
		}
	} else {
		t.Error("Expected an error from scraper.Scrape() but got none")
	}
}

func TestScrape_DoNotScrapeIfNoPodsFound(t *testing.T) {
	client := newTestScrapeClient(testStats, nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("newServiceScraperWithClient=%v, want no error", err)
	}

	// Override the Endpoints with 0 pods.
	createEndpoints(addIps(makeEndpoints(), 0))

	stat, err := scraper.Scrape()
	if err != nil {
		t.Fatalf("got error from scraper.Scrape() = %v", err)
	}
	if stat != nil {
		t.Error("Received unexpected StatMessage.")
	}
}

func serviceScraperForTest(sClient scrapeClient) (*ServiceScraper, error) {
	metric := getTestMetric()
	return newServiceScraperWithClient(metric, kubeInformer.Core().V1().Endpoints().Lister(), sClient)
}

func getTestMetric() *Metric {
	return &Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
			Labels: map[string]string{
				serving.RevisionLabelKey: testRevision,
			},
		},
	}
}

func newTestScrapeClient(stats []*Stat, errs []error) scrapeClient {
	return &fakeScrapeClient{
		stats: stats,
		errs:  errs,
	}
}

type fakeScrapeClient struct {
	i     int
	stats []*Stat
	errs  []error
}

// Scrape return the next item in the stats and error array of fakeScrapeClient.
func (c *fakeScrapeClient) Scrape(url string) (*Stat, error) {
	ans := c.stats[c.i%len(c.stats)]
	err := c.errs[c.i%len(c.errs)]
	c.i++
	return ans, err
}
