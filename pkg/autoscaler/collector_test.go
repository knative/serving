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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	. "github.com/knative/pkg/logging/testing"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	defaultNamespace = "test-namespace"
	defaultName      = "test-name"
	defaultMetric    = &Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaultNamespace,
			Name:      defaultName,
		},
		Spec: MetricSpec{
			StableWindow: 60 * time.Second,
			PanicWindow:  6 * time.Second,
		},
	}
)

func TestMetricCollectorCrud(t *testing.T) {
	defer ClearAll()

	logger := TestLogger(t)
	ctx := context.Background()

	scraper := testScraper(func() (*StatMessage, error) {
		return nil, nil
	})
	factory := scraperFactory(scraper, nil)

	t.Run("error on mismatch", func(t *testing.T) {
		coll := NewMetricCollector(factory, logger)
		coll.Create(ctx, &Metric{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "another-namespace",
				Name:      defaultName,
			},
		})
		if _, err := coll.Get(ctx, defaultNamespace, defaultName); err == nil {
			t.Error("Get() did not return an error")
		}
		if _, err := coll.Update(ctx, defaultMetric); err == nil {
			t.Error("Update() did not return an error")
		}
	})

	t.Run("error on creating scraper", func(t *testing.T) {
		want := errors.New("factory failure")
		failingFactory := scraperFactory(nil, want)

		coll := NewMetricCollector(failingFactory, logger)
		_, got := coll.Create(ctx, defaultMetric)

		if got != want {
			t.Errorf("Create() = %v, want %v", got, want)
		}
	})

	t.Run("full crud", func(t *testing.T) {
		coll := NewMetricCollector(factory, logger)
		coll.Create(ctx, defaultMetric)

		got, err := coll.Get(ctx, defaultNamespace, defaultName)
		if err != nil {
			t.Errorf("Get() = %v, want no error", err)
		}
		if !cmp.Equal(defaultMetric, got) {
			t.Errorf("Get() didn't return the same metric: %v", cmp.Diff(defaultMetric, got))
		}

		got, err = coll.Update(ctx, defaultMetric)
		if err != nil {
			t.Errorf("Update() = %v, want no error", err)
		}
		if !cmp.Equal(defaultMetric, got) {
			t.Errorf("Update() didn't return the same metric: %v", cmp.Diff(defaultMetric, got))
		}

		if err := coll.Delete(ctx, defaultNamespace, defaultName); err != nil {
			t.Errorf("Delete() = %v, want no error", err)
		}
	})
}

func TestMetricCollectorScraper(t *testing.T) {
	defer ClearAll()

	logger := TestLogger(t)
	ctx := context.Background()

	now := time.Now()
	metricKey := NewMetricKey(defaultNamespace, defaultName)
	want := 10.0
	stat := &StatMessage{
		Key: metricKey,
		Stat: Stat{
			Time:                      &now,
			PodName:                   "testPod",
			AverageConcurrentRequests: 10.0,
		},
	}
	scraper := testScraper(func() (*StatMessage, error) {
		return stat, nil
	})
	factory := scraperFactory(scraper, nil)

	coll := NewMetricCollector(factory, logger)
	coll.Create(ctx, defaultMetric)

	// stable concurrency should eventually be equal to the stat.
	var got float64
	wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		got, _, _ = coll.StableAndPanicConcurrency(metricKey)
		return got == want, nil
	})
	if got != want {
		t.Errorf("StableAndPanicConcurrency() = %v, want %v", got, want)
	}

	coll.Delete(ctx, defaultNamespace, defaultName)
	_, _, err := coll.StableAndPanicConcurrency(metricKey)
	if !k8serrors.IsNotFound(err) {
		t.Errorf("StableAndPanicConcurrency() = %v, want a not found error", err)
	}
}

func TestMetricCollectorRecord(t *testing.T) {
	defer ClearAll()

	logger := TestLogger(t)
	ctx := context.Background()

	now := time.Now()
	metricKey := NewMetricKey(defaultNamespace, defaultName)
	want := 10.0
	stat := Stat{
		Time:                             &now,
		PodName:                          "testPod",
		AverageConcurrentRequests:        want + 10,
		AverageProxiedConcurrentRequests: 10, // this should be subtracted from the above.
	}
	scraper := testScraper(func() (*StatMessage, error) {
		return nil, nil
	})
	factory := scraperFactory(scraper, nil)

	coll := NewMetricCollector(factory, logger)

	// Freshly created collection does not contain any metrics and should return an error.
	coll.Create(ctx, defaultMetric)
	if _, _, err := coll.StableAndPanicConcurrency(metricKey); err == nil {
		t.Error("StableAndPanicConcurrency() = nil, wanted an error")
	}

	// After adding a stat the concurrencies are calculated correctly.
	coll.Record(metricKey, stat)
	if stable, panic, err := coll.StableAndPanicConcurrency(metricKey); stable != panic && stable != want && err != nil {
		t.Errorf("StableAndPanicConcurrency() = %v, %v, %v; want %v, %v, nil", stable, panic, err, want, want)
	}
}

func scraperFactory(scraper StatsScraper, err error) StatsScraperFactory {
	return func(*Metric) (StatsScraper, error) {
		return scraper, err
	}
}

type testScraper func() (*StatMessage, error)

func (s testScraper) Scrape() (*StatMessage, error) {
	return s()
}
