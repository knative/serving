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

package queue

import (
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type reportedStat struct {
	Concurrency         float64
	ProxiedConcurrency  float64
	RequestCount        float64
	ProxiedRequestCount float64
}

func TestNoData(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	got := s.report(now)
	want := reportedStat{
		Concurrency:  0.0,
		RequestCount: 0,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestSingleRequestWholeTime(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	s.requestStart(now)
	now = now.Add(1 * time.Second)
	s.requestEnd(now)

	got := s.report(now)

	want := reportedStat{
		Concurrency:  1.0,
		RequestCount: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestSingleRequestHalfTime(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	s.requestStart(now)
	now = now.Add(1 * time.Second)
	s.requestEnd(now)
	now = now.Add(1 * time.Second)
	got := s.report(now)

	want := reportedStat{
		Concurrency:  0.5,
		RequestCount: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestVeryShortLivedRequest(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	s.requestStart(now)
	now = now.Add(10 * time.Millisecond)
	s.requestEnd(now)

	now = now.Add(990 * time.Millisecond) // make the second full
	got := s.report(now)

	want := reportedStat{
		Concurrency:  float64(10) / float64(1000),
		RequestCount: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestMultipleRequestsWholeTime(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	s.requestStart(now)
	now = now.Add(300 * time.Millisecond)
	s.requestEnd(now)

	s.requestStart(now)
	now = now.Add(300 * time.Millisecond)
	s.requestEnd(now)

	s.requestStart(now)
	now = now.Add(400 * time.Millisecond)
	s.requestEnd(now)

	got := s.report(now)

	want := reportedStat{
		Concurrency:  1.0,
		RequestCount: 3,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestMultipleRequestsInterleaved(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	s.requestStart(now)
	now = now.Add(100 * time.Millisecond)
	s.requestStart(now)
	now = now.Add(500 * time.Millisecond)
	s.requestEnd(now)
	now = now.Add(400 * time.Millisecond)
	s.requestEnd(now)

	got := s.report(now)

	want := reportedStat{
		Concurrency:  1.5,
		RequestCount: 2,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestOneRequestAcrossReportings(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)

	s.requestStart(now)
	now = now.Add(1 * time.Second)
	got1 := s.report(now)
	want1 := reportedStat{
		Concurrency:  1.0,
		RequestCount: 1,
	}

	now = now.Add(500 * time.Millisecond)
	s.requestEnd(now)
	now = now.Add(500 * time.Millisecond)
	got2 := s.report(now)
	want2 := reportedStat{
		Concurrency:  0.5,
		RequestCount: 0,
	}

	if diff := cmp.Diff(want1, got1); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
	if diff := cmp.Diff(want2, got2); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestOneProxiedRequest(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)
	s.proxiedStart(now)
	now = now.Add(1 * time.Second)
	got := s.report(now)
	want := reportedStat{
		Concurrency:         1.0,
		ProxiedConcurrency:  1.0,
		RequestCount:        1,
		ProxiedRequestCount: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestOneEndedProxiedRequest(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)
	s.proxiedStart(now)
	now = now.Add(500 * time.Millisecond)
	s.proxiedEnd(now)
	now = now.Add(500 * time.Millisecond)
	got := s.report(now)
	want := reportedStat{
		Concurrency:         0.5,
		ProxiedConcurrency:  0.5,
		RequestCount:        1,
		ProxiedRequestCount: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func approxNeq(a, b float64) bool {
	return math.Abs(a-b) > 0.0001
}

func TestWeightedAverage(t *testing.T) {
	// Tests that weightedAverage works correctly, also helps the
	// function reader to understand what inputs will result in what
	// outputs.

	// Impulse function yields: 1.
	in := map[int]time.Duration{
		1: time.Microsecond,
	}
	if got, want := weightedAverage(in), 1.; approxNeq(got, want) {
		t.Errorf("weightedAverage = %v, want: %v", got, want)
	}

	// Step function
	// Since the times are the same, we'll return:
	// 200*(1+2+3+4+5)/(1000) = 15/5 = 3.
	in = map[int]time.Duration{
		1: 200 * time.Millisecond,
		2: 200 * time.Millisecond,
		3: 200 * time.Millisecond,
		4: 200 * time.Millisecond,
		5: 200 * time.Millisecond,
	}
	if got, want := weightedAverage(in), 3.; approxNeq(got, want) {
		t.Errorf("weightedAverage = %v, want: %v", got, want)
	}

	// Weights matter.
	in = map[int]time.Duration{
		1: 800 * time.Millisecond,
		5: 200 * time.Millisecond,
	}
	// (1*800+5*200)/1000 = 1800/1000 = 1.8
	if got, want := weightedAverage(in), 1.8; approxNeq(got, want) {
		t.Errorf("weightedAverage = %v, want: %v", got, want)
	}

	// Caret.
	in = map[int]time.Duration{
		1: 100 * time.Millisecond,
		2: 200 * time.Millisecond,
		3: 300 * time.Millisecond,
		4: 200 * time.Millisecond,
		5: 100 * time.Millisecond,
	}
	// (100+400+900+800+500)/900 = 3
	if got, want := weightedAverage(in), 3.; approxNeq(got, want) {
		t.Errorf("weightedAverage = %v, want: %v", got, want)
	}

	// Empty.
	in = map[int]time.Duration{}
	if got, want := weightedAverage(in), 0.; approxNeq(got, want) {
		t.Errorf("weightedAverage = %v, want: %v", got, want)
	}
}

func TestTwoRequestsOneProxied(t *testing.T) {
	now := time.Now()
	s := newTestStats(now)
	s.proxiedStart(now)
	now = now.Add(500 * time.Millisecond)
	s.proxiedEnd(now)
	s.requestStart(now)
	now = now.Add(500 * time.Millisecond)
	got := s.report(now)
	want := reportedStat{
		Concurrency:         1.0,
		ProxiedConcurrency:  0.5,
		RequestCount:        2,
		ProxiedRequestCount: 1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

// Test type to hold the bi-directional time channels
type testStats struct {
	reqChan      chan ReqEvent
	reportBiChan chan time.Time
	statChan     chan reportedStat
}

func newTestStats(now time.Time) *testStats {
	reportBiChan := make(chan time.Time)
	reqChan := make(chan ReqEvent)
	statChan := make(chan reportedStat)
	report := func(acr float64, apcr float64, rc float64, prc float64) {
		statChan <- reportedStat{
			Concurrency:         acr,
			ProxiedConcurrency:  apcr,
			RequestCount:        rc,
			ProxiedRequestCount: prc,
		}
	}
	NewStats(now, reqChan, (<-chan time.Time)(reportBiChan), report)
	t := &testStats{
		reqChan:      reqChan,
		reportBiChan: reportBiChan,
		statChan:     statChan,
	}
	return t
}

func (s *testStats) requestStart(now time.Time) {
	s.reqChan <- ReqEvent{Time: now, EventType: ReqIn}
}

func (s *testStats) requestEnd(now time.Time) {
	s.reqChan <- ReqEvent{Time: now, EventType: ReqOut}
}

func (s *testStats) proxiedStart(now time.Time) {
	s.reqChan <- ReqEvent{Time: now, EventType: ProxiedIn}
}

func (s *testStats) proxiedEnd(now time.Time) {
	s.reqChan <- ReqEvent{Time: now, EventType: ProxiedOut}
}

func (s *testStats) report(now time.Time) reportedStat {
	s.reportBiChan <- now
	return <-s.statChan
}
