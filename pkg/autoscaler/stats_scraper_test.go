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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/resources"
)

const (
	testRevision  = "test-revision"
	testService   = "test-revision-metrics"
	testNamespace = "test-namespace"
)

var (
	testStats = []Stat{
		{
			PodName:                          "pod-1",
			AverageConcurrentRequests:        3.0,
			AverageProxiedConcurrentRequests: 2.0,
			RequestCount:                     5,
			ProxiedRequestCount:              4,
		}, {
			PodName:                          "pod-2",
			AverageConcurrentRequests:        5.0,
			AverageProxiedConcurrentRequests: 4.0,
			RequestCount:                     7,
			ProxiedRequestCount:              6,
		}, {
			PodName:                          "pod-3",
			AverageConcurrentRequests:        3.0,
			AverageProxiedConcurrentRequests: 2.0,
			RequestCount:                     5,
			ProxiedRequestCount:              4,
		},
	}
)

func TestNewServiceScraperWithClientHappyCase(t *testing.T) {
	client := newTestScrapeClient(testStats, []error{nil})
	if scraper, err := serviceScraperForTest(client); err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	} else if scraper.url != testURL {
		t.Errorf("scraper.url=%v, want %v", scraper.url, testURL)
	}
}

func TestNewServiceScraperWithClientErrorCases(t *testing.T) {
	metric := testMetric()
	invalidMetric := testMetric()
	invalidMetric.Labels = map[string]string{}
	client := newTestScrapeClient(testStats, []error{nil})
	lister := kubeInformer.Core().V1().Endpoints().Lister()
	counter := resources.NewScopedEndpointsCounter(lister, testNamespace, testService)

	testCases := []struct {
		name        string
		metric      *av1alpha1.Metric
		client      scrapeClient
		counter     resources.ReadyPodCounter
		expectedErr string
	}{{
		name:        "Empty Decider",
		client:      client,
		counter:     counter,
		expectedErr: "metric must not be nil",
	}, {
		name:        "Missing revision label in Decider",
		metric:      invalidMetric,
		client:      client,
		counter:     counter,
		expectedErr: "no Revision label found for Metric test-revision",
	}, {
		name:        "Empty scrape client",
		metric:      metric,
		counter:     counter,
		expectedErr: "scrape client must not be nil",
	}, {
		name:        "Empty lister",
		metric:      metric,
		client:      client,
		counter:     nil,
		expectedErr: "counter must not be nil",
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newServiceScraperWithClient(test.metric, test.counter, test.client); err != nil {
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

func TestScrapeReportStatWhenAllCallsSucceed(t *testing.T) {
	client := newTestScrapeClient(testStats, []error{nil})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	// Make an Endpoints with 3 pods.
	endpoints(3, testService)

	// Scrape will set a timestamp bigger than this.
	now := time.Now()
	got, err := scraper.Scrape()
	if err != nil {
		t.Fatalf("unexpected error from scraper.Scrape(): %v", err)
	}

	if got.Time.Before(now) {
		t.Errorf("stat.Time=%v, want bigger than %v", got.Time, now)
	}
	if got.PodName != scraperPodName {
		t.Errorf("stat.PodName=%v, want %v", got.PodName, scraperPodName)
	}
	// (3.0 + 5.0 + 3.0) / 3.0 * 3 = 11
	if got.AverageConcurrentRequests != 11.0 {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v",
			got.AverageConcurrentRequests, 11.0)
	}
	// ((5 + 7 + 5) / 3.0) * 3 = 17
	if got.RequestCount != 17 {
		t.Errorf("stat.RequestCount=%v, want %v", got.RequestCount, 15)
	}
	// (2.0 + 4.0 + 2.0) / 3.0 * 3 = 8
	if got.AverageProxiedConcurrentRequests != 8.0 {
		t.Errorf("stat.AverageProxiedConcurrentRequests=%v, want %v",
			got.AverageProxiedConcurrentRequests, 8.0)
	}
	// ((4 + 6 + 4) / 3.0) * 3 = 14
	if got.ProxiedRequestCount != 14 {
		t.Errorf("stat.ProxiedCount=%v, want %v", got.ProxiedRequestCount, 12)
	}
}

func TestScrapeReportErrorCannotFindEnoughPods(t *testing.T) {
	client := newTestScrapeClient(testStats[2:], []error{nil})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	// Make an Endpoints with 2 pods.
	endpoints(2, testService)

	_, err = scraper.Scrape()
	if err == nil {
		t.Errorf("scrape.Scrape() = nil, expected an error")
	}
}

func TestScrapeReportErrorIfAnyFails(t *testing.T) {
	errTest := errors.New("test")

	// 1 success and 10 failures so one scrape fails permanently through retries.
	client := newTestScrapeClient(testStats, []error{nil,
		errTest, errTest, errTest, errTest, errTest, errTest, errTest, errTest, errTest, errTest})
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	// Make an Endpoints with 2 pods.
	endpoints(2, testService)

	_, err = scraper.Scrape()
	if !errors.Is(err, errTest) {
		t.Errorf("scraper.Scrape() = %v, want %v wrapped", err, errTest)
	}
}

func TestScrapeDoNotScrapeIfNoPodsFound(t *testing.T) {
	client := newTestScrapeClient(testStats, nil)
	scraper, err := serviceScraperForTest(client)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	// Make an Endpoints with 0 pods.
	endpoints(0, testService)

	stat, err := scraper.Scrape()
	if err != nil {
		t.Fatalf("scraper.Scrape() returned error: %v", err)
	}
	if stat != emptyStat {
		t.Error("Received unexpected Stat.")
	}
}

func serviceScraperForTest(sClient scrapeClient) (*ServiceScraper, error) {
	metric := testMetric()
	counter := resources.NewScopedEndpointsCounter(kubeInformer.Core().V1().Endpoints().Lister(), testNamespace, testService)
	return newServiceScraperWithClient(metric, counter, sClient)
}

func testMetric() *av1alpha1.Metric {
	return &av1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
			Labels: map[string]string{
				serving.RevisionLabelKey: testRevision,
			},
		},
		Spec: av1alpha1.MetricSpec{
			ScrapeTarget: testRevision + "-zhudex",
		},
	}
}

func newTestScrapeClient(stats []Stat, errs []error) scrapeClient {
	return &fakeScrapeClient{
		stats: stats,
		errs:  errs,
	}
}

type fakeScrapeClient struct {
	i     int
	stats []Stat
	errs  []error
	mutex sync.Mutex
}

// Scrape return the next item in the stats and error array of fakeScrapeClient.
func (c *fakeScrapeClient) Scrape(url string) (Stat, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	ans := c.stats[c.i%len(c.stats)]
	err := c.errs[c.i%len(c.errs)]
	c.i++
	return ans, err
}

func TestURLFromTarget(t *testing.T) {
	if got, want := "http://dance.now:9090/metrics", urlFromTarget("dance", "now"); got != want {
		t.Errorf("urlFromTarget = %s, want: %s, diff: %s", got, want, cmp.Diff(got, want))
	}
}
