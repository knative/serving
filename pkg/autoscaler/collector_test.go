/*
Copyright 2019 The Knative Authors.

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
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	. "knative.dev/pkg/logging/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/aggregation"
)

var (
	defaultNamespace = "test-namespace"
	defaultName      = "test-name"
	defaultMetric    = av1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaultNamespace,
			Name:      defaultName,
		},
		Spec: av1alpha1.MetricSpec{
			StableWindow: 60 * time.Second,
			PanicWindow:  6 * time.Second,
			ScrapeTarget: "original-target",
		},
	}
)

func TestMetricCollectorCRUD(t *testing.T) {
	logger := TestLogger(t)

	scraper := &testScraper{
		s: func() (Stat, error) {
			return emptyStat, nil
		},
		url: "just-right",
	}
	scraper2 := &testScraper{
		s: func() (Stat, error) {
			return emptyStat, nil
		},
		url: "slightly-off",
	}
	factory := scraperFactory(scraper, nil)

	t.Run("error on creating scraper", func(t *testing.T) {
		want := errors.New("factory failure")
		failingFactory := scraperFactory(nil, want)

		coll := NewMetricCollector(failingFactory, logger)
		got := coll.CreateOrUpdate(&defaultMetric)

		if got != want {
			t.Errorf("Create() = %v, want %v", got, want)
		}
	})

	t.Run("full crud", func(t *testing.T) {
		key := types.NamespacedName{Namespace: defaultMetric.Namespace, Name: defaultMetric.Name}
		coll := NewMetricCollector(factory, logger)
		if err := coll.CreateOrUpdate(&defaultMetric); err != nil {
			t.Errorf("CreateOrUpdate() = %v, want no error", err)
		}

		got := coll.collections[key].metric
		if !cmp.Equal(&defaultMetric, got) {
			t.Errorf("Get() didn't return the same metric: %v", cmp.Diff(&defaultMetric, got))
		}

		defaultMetric.Spec.ScrapeTarget = "new-target"
		coll.statsScraperFactory = scraperFactory(scraper2, nil)
		if err := coll.CreateOrUpdate(&defaultMetric); err != nil {
			t.Errorf("CreateOrUpdate() = %v, want no error", err)
		}

		got = coll.collections[key].metric
		if !cmp.Equal(&defaultMetric, got) {
			t.Errorf("Update() didn't return the same metric: %v", cmp.Diff(&defaultMetric, got))
		}

		newURL := (coll.collections[key]).scraper.(*testScraper).url
		if got, want := newURL, "slightly-off"; got != want {
			t.Errorf("Updated scraper URL = %s, want: %s, diff: %s", got, want, cmp.Diff(got, want))
		}

		if err := coll.Delete(defaultNamespace, defaultName); err != nil {
			t.Errorf("Delete() = %v, want no error", err)
		}
	})
}

type manualTickProvider struct {
	ch chan time.Time
}

func (mtp *manualTickProvider) NewTicker(time.Duration) *time.Ticker {
	return &time.Ticker{
		C: mtp.ch,
	}
}

func TestMetricCollectorScraper(t *testing.T) {
	logger := TestLogger(t)

	mtp := &manualTickProvider{
		ch: make(chan time.Time),
	}
	now := time.Now()
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const (
		reportConcurrency = 10.0
		reportRPS         = 20.0
		wantConcurrency   = 3 * 10. / 60 // In 3 seconds we'll scrape 3 times, window is 60s.
		wantRPS           = 3 * 20. / 60
		wantPConcurrency  = 3 * 10 / 6.
		wantPRPS          = 3 * 20 / 6.
	)
	stat := Stat{
		Time:                      now,
		PodName:                   "testPod",
		AverageConcurrentRequests: reportConcurrency,
		RequestCount:              reportRPS,
	}
	scraper := &testScraper{
		s: func() (Stat, error) {
			return stat, nil
		},
	}
	factory := scraperFactory(scraper, nil)

	coll := NewMetricCollector(factory, logger)
	coll.tickProvider = mtp.NewTicker // custom ticker.
	coll.CreateOrUpdate(&defaultMetric)

	// Tick three times.  Time doesn't matter since we use the time on the Stat.
	mtp.ch <- now
	mtp.ch <- now
	mtp.ch <- now
	var gotRPS, gotConcurrency, panicRPS, panicConcurrency float64
	// Poll to see that the async loop completed.
	wait.PollImmediate(10*time.Millisecond, 100*time.Millisecond, func() (bool, error) {
		gotConcurrency, _, _ = coll.StableAndPanicConcurrency(metricKey, now)
		gotRPS, _, _ = coll.StableAndPanicRPS(metricKey, now)
		return gotConcurrency == wantConcurrency && gotRPS == wantRPS, nil
	})

	gotConcurrency, panicConcurrency, _ = coll.StableAndPanicConcurrency(metricKey, now)
	gotRPS, panicRPS, err := coll.StableAndPanicRPS(metricKey, now)
	if err != nil {
		t.Errorf("StableAndPanicRPS = %v", err)
	}
	if panicConcurrency != wantPConcurrency {
		t.Errorf("PanicConcurrency() = %v, want %v", panicConcurrency, wantPConcurrency)
	}
	if panicRPS != wantPRPS {
		t.Errorf("PanicRPS() = %v, want %v", panicRPS, wantPRPS)
	}
	if gotConcurrency != wantConcurrency {
		t.Errorf("StableConcurrency() = %v, want %v", gotConcurrency, wantConcurrency)
	}
	if gotRPS != wantRPS {
		t.Errorf("StableRPS() = %v, want %v", gotRPS, wantRPS)
	}

	// Now let's report 2 more values (for a total of 5).
	mtp.ch <- now
	mtp.ch <- now

	// Wait for async loop to finish.
	wait.PollImmediate(10*time.Millisecond, 100*time.Millisecond, func() (bool, error) {
		gotConcurrency, _, _ = coll.StableAndPanicConcurrency(metricKey, now.Add(stableWindow).Add(-5*time.Second))
		gotRPS, _, _ = coll.StableAndPanicRPS(metricKey, now.Add(stableWindow).Add(-5*time.Second))
		return gotConcurrency == reportConcurrency && gotRPS == reportRPS, nil
	})
	if gotConcurrency != reportConcurrency {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", gotConcurrency, wantConcurrency)
	}
	if gotRPS != reportRPS {
		t.Errorf("StableAndPanicRPS() = %v, want %v", gotRPS, wantRPS)
	}

	// Deleting the metric should cause a calculation error.
	coll.Delete(defaultNamespace, defaultName)
	_, _, err = coll.StableAndPanicConcurrency(metricKey, now)
	if err != ErrNotScraping {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", err, ErrNotScraping)
	}
	_, _, err = coll.StableAndPanicRPS(metricKey, now)
	if err != ErrNotScraping {
		t.Errorf("StableAndPanicRPS() = %v, want %v", err, ErrNotScraping)
	}
}

func TestMetricCollectorRecord(t *testing.T) {
	logger := TestLogger(t)

	now := time.Now()
	oldTime := now.Add(-70 * time.Second)
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const want = 10.0
	outdatedStat := Stat{
		Time:                      oldTime,
		PodName:                   "testPod",
		AverageConcurrentRequests: 100,
		RequestCount:              100,
	}
	stat := Stat{
		Time:                             now,
		PodName:                          "testPod",
		AverageConcurrentRequests:        want + 10,
		AverageProxiedConcurrentRequests: 10, // this should be subtracted from the above.
		RequestCount:                     want + 20,
		ProxiedRequestCount:              20, // this should be subtracted from the above.
	}
	scraper := &testScraper{
		s: func() (Stat, error) {
			return emptyStat, nil
		},
	}
	factory := scraperFactory(scraper, nil)

	coll := NewMetricCollector(factory, logger)
	mtp := &manualTickProvider{
		ch: make(chan time.Time),
	}
	coll.tickProvider = mtp.NewTicker // This will ensure time based scraping won't interfere.

	// Freshly created collection does not contain any metrics and should return an error.
	coll.CreateOrUpdate(&defaultMetric)
	if _, _, err := coll.StableAndPanicConcurrency(metricKey, now); err == nil {
		t.Error("StableAndPanicConcurrency() = nil, wanted an error")
	}
	if _, _, err := coll.StableAndPanicRPS(metricKey, now); err == nil {
		t.Error("StableAndPanicRPS() = nil, wanted an error")
	}

	// Add two stats. The second record operation will remove the first outdated one.
	// After this the concurrencies are calculated correctly.
	coll.Record(metricKey, outdatedStat)
	coll.Record(metricKey, stat)
	stable, panic, err := coll.StableAndPanicConcurrency(metricKey, now)
	if err != nil {
		t.Fatalf("StableAndPanicConcurrency: %v", err)
	}
	// Scale to the window sizes.
	const (
		wantS     = want / 60
		wantP     = want / 6
		tolerance = 0.001
	)
	if math.Abs(stable-wantS) > tolerance || math.Abs(panic-wantP) > tolerance {
		t.Errorf("StableAndPanicConcurrency() = %v, %v; want %v, %v, nil", stable, panic, wantS, wantP)
	}
	stable, panic, err = coll.StableAndPanicRPS(metricKey, now)
	if err != nil {
		t.Fatalf("StableAndPanicRPS: %v", err)
	}
	if math.Abs(stable-wantS) > tolerance || math.Abs(panic-wantP) > tolerance {
		t.Errorf("StableAndPanicRPS() = %v, %v; want %v, %v", stable, panic, wantS, wantP)
	}
}

func TestMetricCollectorError(t *testing.T) {
	testCases := []struct {
		name                 string
		scraper              *testScraper
		metric               *av1alpha1.Metric
		expectedMetricStatus duckv1.Status
	}{{
		name: "Failed to get endpoints scraper error",
		scraper: &testScraper{
			s: func() (Stat, error) {
				return emptyStat, ErrFailedGetEndpoints
			},
		},
		metric: &av1alpha1.Metric{
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
		},
		expectedMetricStatus: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:    av1alpha1.MetricConditionReady,
				Status:  corev1.ConditionUnknown,
				Reason:  "NoEndpoints",
				Message: ErrFailedGetEndpoints.Error(),
			}},
		},
	}, {
		name: "Did not receive stat scraper error",
		scraper: &testScraper{
			s: func() (Stat, error) {
				return emptyStat, ErrDidNotReceiveStat
			},
		},
		metric: &av1alpha1.Metric{
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
		},
		expectedMetricStatus: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:    av1alpha1.MetricConditionReady,
				Status:  corev1.ConditionFalse,
				Reason:  "DidNotReceiveStat",
				Message: ErrDidNotReceiveStat.Error(),
			}},
		},
	}, {
		name: "Other scraper error",
		scraper: &testScraper{
			s: func() (Stat, error) {
				return emptyStat, errors.New("foo")
			},
		},
		metric: &av1alpha1.Metric{
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
		},
		expectedMetricStatus: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:    av1alpha1.MetricConditionReady,
				Status:  corev1.ConditionUnknown,
				Reason:  "CreateOrUpdateFailed",
				Message: "Collector has failed.",
			}},
		},
	}}

	logger := TestLogger(t)
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			factory := scraperFactory(test.scraper, nil)
			coll := NewMetricCollector(factory, logger)
			coll.CreateOrUpdate(test.metric)
			key := types.NamespacedName{Namespace: test.metric.Namespace, Name: test.metric.Name}

			var got duckv1.Status
			wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
				collection, ok := coll.collections[key]
				if ok {
					got = collection.currentMetric().Status.Status
					return equality.Semantic.DeepEqual(got, test.expectedMetricStatus), nil
				}
				return false, nil
			})
			if !equality.Semantic.DeepEqual(got, test.expectedMetricStatus) {
				t.Errorf("Got = %#v, want: %#v, diff:\n%q", got, test.expectedMetricStatus, cmp.Diff(got, test.expectedMetricStatus))
			}
			coll.Delete(test.metric.Namespace, test.metric.Name)
		})
	}
}

func scraperFactory(scraper StatsScraper, err error) StatsScraperFactory {
	return func(*av1alpha1.Metric) (StatsScraper, error) {
		return scraper, err
	}
}

type testScraper struct {
	s   func() (Stat, error)
	url string
}

func (s *testScraper) Scrape() (Stat, error) {
	return s.s()
}

func TestMetricCollectorAggregate(t *testing.T) {
	m := defaultMetric
	m.Spec.StableWindow = 6 * time.Second
	m.Spec.PanicWindow = 2 * time.Second
	c := &collection{
		metric:                  &m,
		concurrencyBuckets:      aggregation.NewTimedFloat64Buckets(m.Spec.StableWindow, BucketSize),
		concurrencyPanicBuckets: aggregation.NewTimedFloat64Buckets(m.Spec.PanicWindow, BucketSize),
		rpsBuckets:              aggregation.NewTimedFloat64Buckets(m.Spec.StableWindow, BucketSize),
		rpsPanicBuckets:         aggregation.NewTimedFloat64Buckets(m.Spec.PanicWindow, BucketSize),
	}
	now := time.Now()
	for i := 0; i < 10; i++ {
		stat := Stat{
			Time:                      now.Add(time.Duration(i) * time.Second),
			PodName:                   "testPod",
			AverageConcurrentRequests: float64(i + 5),
			RequestCount:              float64(i + 5),
		}
		c.record(stat)
	}
	st, pan, noData := c.stableAndPanicConcurrency(now.Add(time.Duration(9) * time.Second))
	if noData {
		t.Fatal("Unexpected NoData error")
	}
	if got, want := st, 11.5; got != want {
		t.Errorf("Stable Concurrency = %f, want: %f", got, want)
	}
	if got, want := pan, 13.5; got != want {
		t.Errorf("Stable Concurrency = %f, want: %f", got, want)
	}
}
