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
	"testing"

	"github.com/google/go-cmp/cmp"

	. "github.com/knative/pkg/logging/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMetricCollectorCrud(t *testing.T) {
	logger := TestLogger(t)
	ctx := context.Background()

	defaultNamespace := "test-namespace"
	defaultName := "test-name"
	defaultMetric := &Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaultNamespace,
			Name:      defaultName,
		},
	}
	scraper := testScraper(func() (*StatMessage, error) {
		return nil, nil
	})
	factory := scraperFactory(scraper)
	statsCh := make(chan *StatMessage)

	t.Run("error on mismatch", func(t *testing.T) {
		coll := NewMetricCollector(factory, statsCh, logger)
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

	t.Run("full crud", func(t *testing.T) {
		coll := NewMetricCollector(factory, statsCh, logger)
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

func scraperFactory(scraper StatsScraper) StatsScraperFactory {
	return func(*Metric) (StatsScraper, error) {
		return scraper, nil
	}
}

type testScraper func() (*StatMessage, error)

func (s testScraper) Scrape() (*StatMessage, error) {
	return s()
}
