/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/autoscaler"
)

const (
	podName = "pod"
)

func TestNoData(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 0.0,
		RequestCount:              0,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestOneRequestOneBucket(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart()
	s.requestEnd()
	s.quantize(now)
	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 1.0,
		RequestCount:              1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestLongRequest(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart()
	s.quantize(now)
	now = now.Add(100 * time.Millisecond)
	s.quantize(now)
	s.requestEnd()
	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 1.0,
		RequestCount:              1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestOneRequestMultipleBuckets(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart()
	s.requestEnd()
	s.quantize(now)
	for i := 1; i < 10; i++ {
		now = now.Add(100 * time.Millisecond)
		s.quantize(now)
	}
	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 0.1,
		RequestCount:              1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestManyRequestsOneBucket(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	// Since none of these requests interleave, the reported
	// concurrency should be 1.0
	for i := 0; i < 10; i++ {
		s.requestStart()
		s.requestEnd()
	}
	s.quantize(now)
	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 1.0,
		RequestCount:              10,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestManyRequestsOneBucketInterleaved(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart() // concurrency == 1
	s.requestStart() // concurrency == 2
	s.requestStart() // concurrency == 3
	s.requestEnd()   // concurrency == 2
	s.requestStart() // concurrency == 3
	s.requestStart() // concurrency == 4
	s.requestEnd()   // concurrency == 3
	s.requestEnd()   // concurrency == 2
	s.requestEnd()   // concurrency == 1
	s.requestEnd()   // concurrency == 0
	s.quantize(now)
	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 4.0,
		RequestCount:              5,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func TestOneRequestTwoBuckets(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart()
	s.quantize(now)

	now = now.Add(100 * time.Millisecond)
	s.requestEnd()
	s.quantize(now)
	got := s.report(now)

	want := &autoscaler.Stat{
		Time:                      &now,
		PodName:                   podName,
		AverageConcurrentRequests: 1.0,
		RequestCount:              1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

// Test type to hold the bi-directional time channels
type testStats struct {
	Stats
	quantizationBiChan chan time.Time
	reportBiChan       chan time.Time
}

func newTestStats() *testStats {
	quanitzationBiChan := make(chan time.Time)
	reportBiChan := make(chan time.Time)
	ch := Channels{
		ReqInChan:        make(chan Poke),
		ReqOutChan:       make(chan Poke), // Buffer because ReqOutChan isn't drained until quantization.
		QuantizationChan: (<-chan time.Time)(quanitzationBiChan),
		ReportChan:       (<-chan time.Time)(reportBiChan),
		StatChan:         make(chan *autoscaler.Stat),
	}
	s := NewStats(podName, ch)
	t := &testStats{
		Stats:              *s,
		quantizationBiChan: quanitzationBiChan,
		reportBiChan:       reportBiChan,
	}
	return t
}

func (s *testStats) requestStart() {
	s.ch.ReqInChan <- Poke{}
}

func (s *testStats) requestEnd() {
	s.ch.ReqOutChan <- Poke{}
}

func (s *testStats) quantize(now time.Time) {
	s.quantizationBiChan <- now
}

func (s *testStats) report(t time.Time) *autoscaler.Stat {
	s.reportBiChan <- t
	return <-s.ch.StatChan
}

func assertTime(t *testing.T, stat *autoscaler.Stat, expectTime time.Time) {
	if *stat.Time != expectTime {
		t.Errorf("Time was incorrect. Expected %v. Got %v.", expectTime, stat.Time)
	}
}

func assertPodName(t *testing.T, stat *autoscaler.Stat) {
	if stat.PodName != podName {
		t.Errorf("Pod name was incorrect. Expected %v. Got %v.", podName, stat.PodName)
	}
}

func assertConcurrency(t *testing.T, stat *autoscaler.Stat, avg float64) {
	if stat.AverageConcurrentRequests != avg {
		t.Errorf("Average concurrent requests was incorrect. Expected %v. Got %v.", avg, stat.AverageConcurrentRequests)
	}
}

func assertQPS(t *testing.T, stat *autoscaler.Stat, qps int32) {
	if stat.RequestCount != qps {
		t.Errorf("Total requests this period was incorrect. Expected %v. Got %v.", qps, stat.RequestCount)
	}
}
