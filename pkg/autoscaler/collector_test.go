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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	. "knative.dev/pkg/logging/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
)

var (
	defaultNamespace = "test-namespace"
	defaultName      = "test-name"
	defaultMetric    = &av1alpha1.Metric{
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
	defer ClearAll()
	logger := TestLogger(t)

	scraper := &testScraper{
		s: func() (*StatMessage, error) {
			return nil, nil
		},
		url: "just-right",
	}
	scraper2 := &testScraper{
		s: func() (*StatMessage, error) {
			return nil, nil
		},
		url: "slightly-off",
	}
	factory := scraperFactory(scraper, nil)

	t.Run("error on creating scraper", func(t *testing.T) {
		want := errors.New("factory failure")
		failingFactory := scraperFactory(nil, want)

		coll := NewMetricCollector(failingFactory, logger)
		got := coll.CreateOrUpdate(defaultMetric)

		if got != want {
			t.Errorf("Create() = %v, want %v", got, want)
		}
	})

	t.Run("full crud", func(t *testing.T) {
		key := types.NamespacedName{Namespace: defaultMetric.Namespace, Name: defaultMetric.Name}
		coll := NewMetricCollector(factory, logger)
		if err := coll.CreateOrUpdate(defaultMetric); err != nil {
			t.Errorf("CreateOrUpdate() = %v, want no error", err)
		}

		got := coll.collections[key].metric
		if !cmp.Equal(defaultMetric, got) {
			t.Errorf("Get() didn't return the same metric: %v", cmp.Diff(defaultMetric, got))
		}

		defaultMetric.Spec.ScrapeTarget = "new-target"
		coll.statsScraperFactory = scraperFactory(scraper2, nil)
		if err := coll.CreateOrUpdate(defaultMetric); err != nil {
			t.Errorf("CreateOrUpdate() = %v, want no error", err)
		}

		got = coll.collections[key].metric
		if !cmp.Equal(defaultMetric, got) {
			t.Errorf("Update() didn't return the same metric: %v", cmp.Diff(defaultMetric, got))
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

func TestMetricCollectorScraper(t *testing.T) {
	defer ClearAll()
	logger := TestLogger(t)

	now := time.Now()
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	wantConcurrency := 10.0
	wantOPS := 20.0
	stat := &StatMessage{
		Key: metricKey,
		Stat: Stat{
			Time:                      &now,
			PodName:                   "testPod",
			AverageConcurrentRequests: wantConcurrency,
			RequestCount:              wantOPS,
		},
	}
	scraper := &testScraper{
		s: func() (*StatMessage, error) {
			return stat, nil
		},
	}
	factory := scraperFactory(scraper, nil)

	coll := NewMetricCollector(factory, logger)
	coll.CreateOrUpdate(defaultMetric)

	// stable concurrency and OPS should eventually be equal to the stat.
	var gotConcurrency, gotOPS float64
	wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		gotConcurrency, _, _ = coll.StableAndPanicConcurrency(metricKey, now)
		gotOPS, _, _ = coll.StableAndPanicOPS(metricKey, now)
		return gotConcurrency == wantConcurrency && gotOPS == wantOPS, nil
	})
	if gotConcurrency != wantConcurrency {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", gotConcurrency, wantConcurrency)
	}
	if gotOPS != wantOPS {
		t.Errorf("StableAndPanicOPS() = %v, want %v", gotOPS, wantOPS)
	}

	// injecting times inside the window should not change the calculation result
	wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
		gotConcurrency, _, _ = coll.StableAndPanicConcurrency(metricKey, now.Add(stableWindow).Add(-5*time.Second))
		gotOPS, _, _ = coll.StableAndPanicOPS(metricKey, now.Add(stableWindow).Add(-5*time.Second))
		return gotConcurrency == wantConcurrency && gotOPS == wantOPS, nil
	})
	if gotConcurrency != wantConcurrency {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", gotConcurrency, wantConcurrency)
	}
	if gotOPS != wantOPS {
		t.Errorf("StableAndPanicOPS() = %v, want %v", gotOPS, wantOPS)
	}

	// deleting the metric should cause a calculation error
	coll.Delete(defaultNamespace, defaultName)
	_, _, err := coll.StableAndPanicConcurrency(metricKey, now)
	if err != ErrNotScraping {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", err, ErrNotScraping)
	}
	_, _, err = coll.StableAndPanicOPS(metricKey, now)
	if err != ErrNotScraping {
		t.Errorf("StableAndPanicOPS() = %v, want %v", err, ErrNotScraping)
	}
}

func TestMetricCollectorRecord(t *testing.T) {
	defer ClearAll()
	logger := TestLogger(t)

	now := time.Now()
	metricKey := types.NamespacedName{Namespace: defaultNamespace, Name: defaultName}
	want := 10.0
	stat := Stat{
		Time:                             &now,
		PodName:                          "testPod",
		AverageConcurrentRequests:        want + 10,
		AverageProxiedConcurrentRequests: 10, // this should be subtracted from the above.
		RequestCount:                     want + 20,
		ProxiedRequestCount:              20, // this should be subtracted from the above.
	}
	scraper := &testScraper{
		s: func() (*StatMessage, error) {
			return nil, nil
		},
	}
	factory := scraperFactory(scraper, nil)

	coll := NewMetricCollector(factory, logger)

	// Freshly created collection does not contain any metrics and should return an error.
	coll.CreateOrUpdate(defaultMetric)
	if _, _, err := coll.StableAndPanicConcurrency(metricKey, now); err == nil {
		t.Error("StableAndPanicConcurrency() = nil, wanted an error")
	}
	if _, _, err := coll.StableAndPanicOPS(metricKey, now); err == nil {
		t.Error("StableAndPanicOPS() = nil, wanted an error")
	}

	// After adding a stat the concurrencies are calculated correctly.
	coll.Record(metricKey, stat)
	if stable, panic, err := coll.StableAndPanicConcurrency(metricKey, now); stable != panic && stable != want && err != nil {
		t.Errorf("StableAndPanicConcurrency() = %v, %v, %v; want %v, %v, nil", stable, panic, err, want, want)
	}
	if stable, panic, err := coll.StableAndPanicOPS(metricKey, now); stable != panic && stable != want && err != nil {
		t.Errorf("StableAndPanicConcurrency() = %v, %v, %v; want %v, %v, nil", stable, panic, err, want, want)
	}
}

func scraperFactory(scraper StatsScraper, err error) StatsScraperFactory {
	return func(*av1alpha1.Metric) (StatsScraper, error) {
		return scraper, err
	}
}

type testScraper struct {
	s   func() (*StatMessage, error)
	url string
}

func (s *testScraper) Scrape() (*StatMessage, error) {
	return s.s()
}
