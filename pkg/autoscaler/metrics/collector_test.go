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
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"

	. "knative.dev/pkg/logging/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/aggregation"
	"knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/autoscaler/fake"
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

		if !errors.Is(got, want) {
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
			t.Error("Get() didn't return the same metric:", cmp.Diff(&defaultMetric, got))
		}

		defaultMetric.Spec.ScrapeTarget = "new-target"
		coll.statsScraperFactory = scraperFactory(scraper2, nil)
		if err := coll.CreateOrUpdate(&defaultMetric); err != nil {
			t.Errorf("CreateOrUpdate() = %v, want no error", err)
		}

		got = coll.collections[key].metric
		if !cmp.Equal(&defaultMetric, got) {
			t.Error("Update() didn't return the same metric:", cmp.Diff(&defaultMetric, got))
		}

		newURL := (coll.collections[key]).scraper.(*testScraper).url
		if got, want := newURL, "slightly-off"; got != want {
			t.Errorf("Updated scraper URL = %s, want: %s, diff: %s", got, want, cmp.Diff(got, want))
		}

		coll.Delete(defaultNamespace, defaultName)
	})
}

func TestMetricCollectorScraperMovingTime(t *testing.T) {
	logger := TestLogger(t)

	mtp := &fake.ManualTickProvider{
		Channel: make(chan time.Time),
	}
	now := time.Now()
	fc := fake.Clock{
		FakeClock: clock.NewFakeClock(now),
		TP:        mtp,
	}
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const (
		reportConcurrency = 10
		reportRPS         = 20
		wantConcurrency   = 7 * 10 / 2
		wantRPS           = 7 * 20 / 2
		wantPConcurrency  = 7 * 10 / 2
		wantPRPS          = 7 * 20 / 2
	)
	stat := Stat{
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
	coll.clock = fc
	coll.CreateOrUpdate(&defaultMetric)

	// Tick three times.  Time doesn't matter since we use the time on the Stat.
	for i := 0; i < 3; i++ {
		mtp.Channel <- now
	}
	now = now.Add(time.Second)
	fc.SetTime(now)
	for i := 0; i < 4; i++ {
		mtp.Channel <- now
	}
	var gotRPS, gotConcurrency, panicRPS, panicConcurrency float64
	// Poll to see that the async loop completed.
	wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		gotConcurrency, panicConcurrency, _ = coll.StableAndPanicConcurrency(metricKey, now)
		gotRPS, panicRPS, _ = coll.StableAndPanicRPS(metricKey, now)
		return gotConcurrency == wantConcurrency &&
			panicConcurrency == wantPConcurrency &&
			gotRPS == wantRPS &&
			panicRPS == wantPRPS, nil
	})

	if _, _, err := coll.StableAndPanicConcurrency(metricKey, now); err != nil {
		t.Error("StableAndPanicConcurrency() =", err)
	}
	if _, _, err := coll.StableAndPanicRPS(metricKey, now); err != nil {
		t.Error("StableAndPanicRPS() =", err)
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
}

func TestMetricCollectorScraper(t *testing.T) {
	logger := TestLogger(t)

	mtp := &fake.ManualTickProvider{
		Channel: make(chan time.Time),
	}
	now := time.Now()
	fc := fake.Clock{
		FakeClock: clock.NewFakeClock(now),
		TP:        mtp,
	}
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const (
		reportConcurrency = 10
		reportRPS         = 20
		wantConcurrency   = 3 * 10 // In 3 seconds we'll scrape 3 times.
		wantRPS           = 3 * 20
		wantPConcurrency  = 3 * 10
		wantPRPS          = 3 * 20
	)
	stat := Stat{
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
	coll.clock = fc
	coll.CreateOrUpdate(&defaultMetric)

	// Tick three times.  Time doesn't matter since we use the time on the Stat.
	for i := 0; i < 3; i++ {
		mtp.Channel <- now
	}
	var gotRPS, gotConcurrency, panicRPS, panicConcurrency float64
	// Poll to see that the async loop completed.
	wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		gotConcurrency, panicConcurrency, _ = coll.StableAndPanicConcurrency(metricKey, now)
		gotRPS, panicRPS, _ = coll.StableAndPanicRPS(metricKey, now)
		return gotConcurrency == wantConcurrency &&
			panicConcurrency == wantPConcurrency &&
			gotRPS == wantRPS &&
			panicRPS == wantPRPS, nil
	})

	if _, _, err := coll.StableAndPanicConcurrency(metricKey, now); err != nil {
		t.Error("StableAndPanicConcurrency() =", err)
	}
	if _, _, err := coll.StableAndPanicRPS(metricKey, now); err != nil {
		t.Error("StableAndPanicRPS() =", err)
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
	mtp.Channel <- now
	mtp.Channel <- now

	// Wait for async loop to finish.
	if err := wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		gotConcurrency, _, _ = coll.StableAndPanicConcurrency(metricKey, now.Add(defaultMetric.Spec.StableWindow).Add(-5*time.Second))
		gotRPS, _, _ = coll.StableAndPanicRPS(metricKey, now.Add(defaultMetric.Spec.StableWindow).Add(-5*time.Second))
		return gotConcurrency == reportConcurrency*5 && gotRPS == reportRPS*5, nil
	}); err != nil {
		t.Fatalf("Timed out waiting for the expected values to appear: CC = %v, RPS = %v", gotConcurrency, gotRPS)
	}
	if gotConcurrency != reportConcurrency*5 {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", gotConcurrency, reportConcurrency*5)
	}
	if gotRPS != reportRPS*5 {
		t.Errorf("StableAndPanicRPS() = %v, want %v", gotRPS, reportRPS*5)
	}

	// Deleting the metric should cause a calculation error.
	coll.Delete(defaultNamespace, defaultName)
	if _, _, err := coll.StableAndPanicConcurrency(metricKey, now); !errors.Is(err, ErrNotCollecting) {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", err, ErrNotCollecting)
	}
	if _, _, err := coll.StableAndPanicRPS(metricKey, now); !errors.Is(err, ErrNotCollecting) {
		t.Errorf("StableAndPanicRPS() = %v, want %v", err, ErrNotCollecting)
	}
}

func TestMetricCollectorNoScraper(t *testing.T) {
	logger := TestLogger(t)

	mtp := &fake.ManualTickProvider{
		Channel: make(chan time.Time),
	}
	now := time.Now()
	fc := fake.Clock{
		FakeClock: clock.NewFakeClock(now),
		TP:        mtp,
	}
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const wantStat = 0.
	stat := Stat{
		PodName:                   "testPod",
		AverageConcurrentRequests: wantStat,
		RequestCount:              wantStat,
	}
	factory := scraperFactory(nil, nil)

	coll := NewMetricCollector(factory, logger)
	coll.clock = fc

	noTargetMetric := defaultMetric
	noTargetMetric.Spec.ScrapeTarget = ""
	coll.CreateOrUpdate(&noTargetMetric)
	// Tick three times.  Time doesn't matter since we use the time on the Stat.
	for i := 0; i < 3; i++ {
		mtp.Channel <- now
		now = now.Add(time.Second)
		fc.SetTime(now)
	}

	gotConcurrency, panicConcurrency, errCon := coll.StableAndPanicConcurrency(metricKey, now)
	gotRPS, panicRPS, errRPS := coll.StableAndPanicRPS(metricKey, now)
	if errCon != nil {
		t.Error("StableAndPanicConcurrency() =", errCon)
	}
	if errRPS != nil {
		t.Error("StableAndPanicRPS() =", errRPS)
	}
	if panicConcurrency != wantStat {
		t.Errorf("PanicConcurrency() = %v, want %v", panicConcurrency, wantStat)
	}
	if panicRPS != wantStat {
		t.Errorf("PanicRPS() = %v, want %v", panicRPS, wantStat)
	}
	if gotConcurrency != wantStat {
		t.Errorf("StableConcurrency() = %v, want %v", gotConcurrency, wantStat)
	}
	if gotRPS != wantStat {
		t.Errorf("StableRPS() = %v, want %v", gotRPS, wantStat)
	}

	// Verify Record() works as expected and values can be retrieved.
	const (
		wantRC        = 30.0
		wantAverageRC = 10.0
	)
	stat.RequestCount = wantRC
	stat.AverageConcurrentRequests = wantAverageRC

	coll.Record(metricKey, now, stat)

	gotConcurrency, _, _ = coll.StableAndPanicConcurrency(metricKey, now)
	gotRPS, _, err := coll.StableAndPanicRPS(metricKey, now)
	if err != nil {
		t.Error("StableAndPanicRPS() =", err)
	}
	if gotRPS != wantRC {
		t.Errorf("StableRPS() = %v, want %v", gotRPS, wantRC)
	}
	if gotConcurrency != wantAverageRC {
		t.Errorf("StableConcurrency() = %v, want %v", gotConcurrency, wantAverageRC)
	}
}

func TestMetricCollectorNoDataError(t *testing.T) {
	logger := TestLogger(t)

	now := time.Now()
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const wantStat = 0.
	stat := Stat{
		PodName:                   "testPod",
		AverageConcurrentRequests: wantStat,
		RequestCount:              wantStat,
	}
	scraper := &testScraper{
		s: func() (Stat, error) {
			return stat, nil
		},
	}
	factory := scraperFactory(scraper, nil)
	coll := NewMetricCollector(factory, logger)

	coll.CreateOrUpdate(&defaultMetric)
	// Verify correct error is returned if ScrapeTarget is set
	_, _, errCon := coll.StableAndPanicConcurrency(metricKey, now)
	_, _, errRPS := coll.StableAndPanicRPS(metricKey, now)
	if !errors.Is(errCon, ErrNoData) {
		t.Error("StableAndPanicConcurrency() =", errCon)
	}
	if !errors.Is(errRPS, ErrNoData) {
		t.Error("StableAndPanicRPS() =", errRPS)
	}
}

func TestMetricCollectorRecord(t *testing.T) {
	logger := TestLogger(t)

	now := time.Now()
	oldTime := now.Add(-70 * time.Second)
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	const want = 10.0
	outdatedStat := Stat{
		PodName:                   "testPod",
		AverageConcurrentRequests: 100,
		RequestCount:              100,
	}
	stat := Stat{
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
	mtp := &fake.ManualTickProvider{
		Channel: make(chan time.Time),
	}
	fc := fake.Clock{
		FakeClock: clock.NewFakeClock(now),
		TP:        mtp,
	}
	coll.clock = fc

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
	coll.Record(metricKey, oldTime, outdatedStat)
	coll.Record(metricKey, now, stat)
	stable, panic, err := coll.StableAndPanicConcurrency(metricKey, now)
	if err != nil {
		t.Fatal("StableAndPanicConcurrency:", err)
	}
	// Scale to the window sizes.
	const (
		wantS     = want
		wantP     = want
		tolerance = 0.001
	)
	if math.Abs(stable-wantS) > tolerance || math.Abs(panic-wantP) > tolerance {
		t.Errorf("StableAndPanicConcurrency() = %v, %v; want %v, %v, nil", stable, panic, wantS, wantP)
	}
	stable, panic, err = coll.StableAndPanicRPS(metricKey, now)
	if err != nil {
		t.Fatal("StableAndPanicRPS:", err)
	}
	if math.Abs(stable-wantS) > tolerance || math.Abs(panic-wantP) > tolerance {
		t.Errorf("StableAndPanicRPS() = %v, %v; want %v, %v", stable, panic, wantS, wantP)
	}
}

func TestDoubleWatch(t *testing.T) {
	defer func() {
		if x := recover(); x == nil {
			t.Error("Expected panic")
		}
	}()
	logger := TestLogger(t)
	factory := scraperFactory(nil, nil)
	coll := NewMetricCollector(factory, logger)
	coll.Watch(func(types.NamespacedName) {})
	coll.Watch(func(types.NamespacedName) {})
}

func TestMetricCollectorError(t *testing.T) {
	testMetric := &av1alpha1.Metric{
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

	errOther := errors.New("foo")

	testCases := []struct {
		name          string
		scraper       *testScraper
		expectedError error
	}{{
		name: "Failed to get endpoints scraper error",
		scraper: &testScraper{
			s: func() (Stat, error) {
				return emptyStat, ErrFailedGetEndpoints
			},
		},
		expectedError: ErrFailedGetEndpoints,
	}, {
		name: "Did not receive stat scraper error",
		scraper: &testScraper{
			s: func() (Stat, error) {
				return emptyStat, ErrDidNotReceiveStat
			},
		},
		expectedError: ErrDidNotReceiveStat,
	}, {
		name: "Other scraper error",
		scraper: &testScraper{
			s: func() (Stat, error) {
				return emptyStat, errOther
			},
		},
		expectedError: errOther,
	}}

	logger := TestLogger(t)
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			factory := scraperFactory(test.scraper, nil)
			mtp := &fake.ManualTickProvider{
				Channel: make(chan time.Time),
			}
			now := time.Now()
			fc := fake.Clock{
				FakeClock: clock.NewFakeClock(now),
				TP:        mtp,
			}
			coll := NewMetricCollector(factory, logger)
			coll.clock = fc

			watchCh := make(chan types.NamespacedName)
			coll.Watch(func(key types.NamespacedName) {
				watchCh <- key
			})

			// Create a collection and immediately tick.
			coll.CreateOrUpdate(testMetric)
			mtp.Channel <- now

			// Expect an event to be propagated because we're erroring.
			key := types.NamespacedName{Namespace: testMetric.Namespace, Name: testMetric.Name}
			event := <-watchCh
			if event != key {
				t.Fatalf("Event = %v, want %v", event, key)
			}

			// Make sure the error is surfaced via 'CreateOrUpdate', which is called in the reconciler.
			if err := coll.CreateOrUpdate(testMetric); !errors.Is(err, test.expectedError) {
				t.Fatalf("CreateOrUpdate = %v, want %v", err, test.expectedError)
			}

			coll.Delete(testMetric.Namespace, testMetric.Name)
		})
	}
}

func scraperFactory(scraper StatsScraper, err error) StatsScraperFactory {
	return func(*av1alpha1.Metric, *zap.SugaredLogger) (StatsScraper, error) {
		return scraper, err
	}
}

type testScraper struct {
	s   func() (Stat, error)
	url string
}

func (s *testScraper) Scrape(time.Duration) (Stat, error) {
	return s.s()
}

func TestMetricCollectorAggregate(t *testing.T) {
	m := defaultMetric
	m.Spec.StableWindow = 6 * time.Second
	m.Spec.PanicWindow = 2 * time.Second
	c := &collection{
		metric:                  &m,
		concurrencyBuckets:      aggregation.NewTimedFloat64Buckets(m.Spec.StableWindow, config.BucketSize),
		concurrencyPanicBuckets: aggregation.NewTimedFloat64Buckets(m.Spec.PanicWindow, config.BucketSize),
		rpsBuckets:              aggregation.NewTimedFloat64Buckets(m.Spec.StableWindow, config.BucketSize),
		rpsPanicBuckets:         aggregation.NewTimedFloat64Buckets(m.Spec.PanicWindow, config.BucketSize),
	}
	now := time.Now()
	for i := time.Duration(0); i < 10; i++ {
		stat := Stat{
			PodName:                   "testPod",
			AverageConcurrentRequests: float64(i + 5),
			RequestCount:              float64(i + 5),
		}
		c.record(now.Add(i*time.Second), stat)
	}

	now = now.Add(9 * time.Second)
	if c.concurrencyBuckets.IsEmpty(now) {
		t.Fatal("Unexpected NoData error")
	}
	if got, want := c.concurrencyBuckets.WindowAverage(now), 11.5; got != want {
		t.Errorf("Stable Concurrency = %f, want: %f", got, want)
	}
	if got, want := c.concurrencyPanicBuckets.WindowAverage(now), 13.5; got != want {
		t.Errorf("Stable Concurrency = %f, want: %f", got, want)
	}
}
