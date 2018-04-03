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
	stat := s.report()
	assertPodName(t, stat)
	assertAverageConcurrentRequests(t, stat, 0.0)
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
		ReqOutChan:       make(chan Poke),
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

func (s *testStats) report() *autoscaler.Stat {
	s.reportBiChan <- time.Now()
	return <-s.ch.StatChan
}

func assertPodName(t *testing.T, stat *autoscaler.Stat) {
	if stat.PodName != podName {
		t.Errorf("Pod name was incorrect. Expected %v. Got %v.", podName, stat.PodName)
	}
}

func assertAverageConcurrentRequests(t *testing.T, stat *autoscaler.Stat, avg float64) {
	if stat.AverageConcurrentRequests != avg {
		t.Errorf("Average concurrent requests was incorrect. Expected %v. Got %v.", avg, stat.AverageConcurrentRequests)
	}
}
