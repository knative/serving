/*
Copyright 2020 The Knative Authors

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

package queue

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"knative.dev/serving/pkg/autoscaler/metrics"
)

func TestProtobufStatsReporterReport(t *testing.T) {
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			reporter := NewProtobufStatsReporter(pod, test.reportingPeriod)
			// Make the value slightly more interesting, rather than microseconds.
			reporter.startTime = reporter.startTime.Add(-5 * time.Second)
			reporter.Report(test.report)
			got := scrapeProtobufStat(t, reporter)
			test.want.PodName = pod
			if !cmp.Equal(test.want, got, ignoreStatFields) {
				t.Errorf("Scraped stat mismatch; diff(-want,+got):\n%s", cmp.Diff(test.want, got))
			}
			if gotUptime := got.ProcessUptime; gotUptime < 5.0 || gotUptime > 6.0 {
				t.Errorf("Got %v for process uptime, wanted 5.0 <= x < 6.0", gotUptime)
			}
		})
	}
}

func TestInitialProtobufStateValid(t *testing.T) {
	r := NewProtobufStatsReporter(pod, 1*time.Second)
	emptyStat := metrics.Stat{
		PodName: pod,
	}

	// test that scraping before we called Report returns an empty
	// stat rather than an error. We don't want to accidentally fall
	// back to mesh mode or something if we manage to scrape too early.
	got := scrapeProtobufStat(t, r)
	if !cmp.Equal(emptyStat, got) {
		t.Errorf("Scraped stat mismatch; diff(-want,+got):\n%s", cmp.Diff(emptyStat, got))
	}
}

func scrapeProtobufStat(t *testing.T, r *ProtobufStatsReporter) metrics.Stat {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, nil)
	result := w.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("Expected ServeHTTP status %d but was %d", http.StatusOK, result.StatusCode)
	}

	b, err := ioutil.ReadAll(result.Body)
	if err != nil {
		t.Fatal("Expected Read to succeed, got", err)
	}

	var stat metrics.Stat
	if err := stat.Unmarshal(b); err != nil {
		t.Fatal("Expected Unmarshal to succeed, got", err)
	}

	return stat
}
