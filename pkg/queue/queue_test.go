package queue

import (
	"testing"
	"time"

	"github.com/elafros/elafros/pkg/autoscaler"
)

const (
	podName = "pod"
)

func TestNoData(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	stat := s.report(now)

	assertTime(t, stat, now)
	assertPodName(t, stat)
	assertConcurrency(t, stat, 0.0)
	assertQps(t, stat, 0)
}

func TestOneRequestOneBucket(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart()
	s.requestEnd()
	s.quantize(now)
	stat := s.report(now)

	assertTime(t, stat, now)
	assertPodName(t, stat)
	assertConcurrency(t, stat, 1.0)
	assertQps(t, stat, 1)
}

func TestLongRequest(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	s.requestStart()
	s.quantize(now)
	now = now.Add(100 * time.Millisecond)
	s.quantize(now)
	s.requestEnd()
	stat := s.report(now)

	assertTime(t, stat, now)
	assertPodName(t, stat)
	assertConcurrency(t, stat, 1.0)
	assertQps(t, stat, 1)
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
	stat := s.report(now)

	assertTime(t, stat, now)
	assertPodName(t, stat)
	assertConcurrency(t, stat, 0.1)
	assertQps(t, stat, 1)
}

func TestManyRequestsOneBucket(t *testing.T) {
	s := newTestStats()
	now := time.Now()

	for i := 0; i < 10; i++ {
		s.requestStart()
		s.requestEnd()
	}
	s.quantize(now)
	stat := s.report(now)

	assertTime(t, stat, now)
	assertPodName(t, stat)
	assertConcurrency(t, stat, 10.0)
	assertQps(t, stat, 10)
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
		ReqOutChan:       make(chan Poke, 100), // Buffer because ReqOutChan isn't drained until quantization.
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

func assertQps(t *testing.T, stat *autoscaler.Stat, qps int32) {
	if stat.RequestCount != qps {
		t.Errorf("Total requests this period was incorrect. Expected %v. Got %v.", qps, stat.RequestCount)
	}
}
