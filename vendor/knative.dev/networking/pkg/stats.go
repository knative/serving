/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pkg

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// ReqEvent represents either an incoming or closed request.
// +k8s:deepcopy-gen=false
type ReqEvent struct {
	// Time is the time the request event happened.
	Time time.Time
	// Type is the type of the request event.
	Type ReqEventType
	// Key is the revision the event is associated with.
	// +optional
	Key types.NamespacedName
}

// ReqEventType denotes the type (incoming/closed) of a ReqEvent.
type ReqEventType int

const (
	// ReqIn represents an incoming request
	ReqIn ReqEventType = iota
	// ReqOut represents a finished request
	ReqOut
	// ProxiedIn represents an incoming request through a proxy.
	ProxiedIn
	// ProxiedOut represents a finished proxied request.
	ProxiedOut
)

// NewRequestStats builds a RequestStats instance, started at the given time.
func NewRequestStats(startedAt time.Time) *RequestStats {
	return &RequestStats{lastChange: startedAt}
}

// RequestStats collects statistics about requests as they flow in and out of the system.
// +k8s:deepcopy-gen=false
type RequestStats struct {
	mux sync.Mutex

	// State variables that track the current state. Not reset after reporting.
	concurrency, proxiedConcurrency float64
	lastChange                      time.Time

	// Reporting variables that track state over the current window. Reset after
	// reporting.
	requestCount, proxiedCount                      float64
	computedConcurrency, computedProxiedConcurrency float64
	secondsInUse                                    float64
}

// RequestStatsReport are the metrics reported from the the request stats collector
// at a given time.
// +k8s:deepcopy-gen=false
type RequestStatsReport struct {
	// AverageConcurrency is the average concurrency over the reporting timeframe.
	// This is calculated via the utilization at a given concurrency. For example:
	// 2 requests each taking 500ms over a 1s reporting window generate an average
	// concurrency of 1.
	AverageConcurrency float64
	// AverageProxiedConcurrency is the average concurrency of all proxied requests.
	// The same calculation as above applies.
	AverageProxiedConcurrency float64
	// RequestCount is the number of requests that arrived in the current reporting
	// timeframe.
	RequestCount float64
	// ProxiedRequestCount is the number of proxied requests that arrived in the current
	// reporting timeframe.
	ProxiedRequestCount float64
}

// compute updates the internal state since the last computed change.
//
// Note: Due to the async nature in which compute can be called, for
// example via HandleEvent and Report, the individual timestamps are not
// guaranteed to be monotonic. We ignore negative changes as they are likely
// benign and are rounding errors at most if the proposed pattern is used.
func (s *RequestStats) compute(now time.Time) {
	if durationSinceChange := now.Sub(s.lastChange); durationSinceChange > 0 {
		durationSecs := durationSinceChange.Seconds()
		s.secondsInUse += durationSecs
		s.computedConcurrency += s.concurrency * durationSecs
		s.computedProxiedConcurrency += s.proxiedConcurrency * durationSecs
		s.lastChange = now
	}
}

// HandleEvent handles an incoming or outgoing request event and updates
// the state accordingly.
func (s *RequestStats) HandleEvent(event ReqEvent) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.compute(event.Time)

	switch event.Type {
	case ProxiedIn:
		s.proxiedConcurrency++
		s.proxiedCount++
		fallthrough
	case ReqIn:
		s.requestCount++
		s.concurrency++
	case ProxiedOut:
		s.proxiedConcurrency--
		fallthrough
	case ReqOut:
		s.concurrency--
	}
}

// Report returns a RequestStatsReport relative to the given time. The state
// will be reset for another reporting cycle afterwards.
func (s *RequestStats) Report(now time.Time) RequestStatsReport {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.compute(now)
	defer s.reset()

	report := RequestStatsReport{
		RequestCount:        s.requestCount,
		ProxiedRequestCount: s.proxiedCount,
	}

	if s.secondsInUse > 0 {
		report.AverageConcurrency = s.computedConcurrency / s.secondsInUse
		report.AverageProxiedConcurrency = s.computedProxiedConcurrency / s.secondsInUse
	}
	return report
}

// reset resets the state so a new reporting cycle can start.
func (s *RequestStats) reset() {
	s.computedConcurrency, s.computedProxiedConcurrency = 0, 0
	s.requestCount, s.proxiedCount = 0, 0
	s.secondsInUse = 0
}
