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

package network

import (
	"time"
)

// ReqEvent represents either an incoming or closed request.
// +k8s:deepcopy-gen=false
type ReqEvent struct {
	Time time.Time
	Type ReqEventType
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
	// State variables that track the current state. Not reset after reporting.
	concurrency, proxiedConcurrency float64
	lastChange                      time.Time

	// Reporting variables that track state over the current window. Reset after
	// reporting.
	requestCount, proxiedCount                      float64
	computedConcurrency, computedProxiedConcurrency float64
	secondsInUse                                    float64
}

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

// Report returns averageConcurrency, averageProxiedConcurrency, requestCount and proxiedCount
// relative to the given time. The state will be reset for another reporting cycle afterwards.
func (s *RequestStats) Report(now time.Time) (avgC float64, avgPC float64, rC float64, pC float64) {
	s.compute(now)
	defer s.reset()

	if s.secondsInUse > 0 {
		avgC = s.computedConcurrency / s.secondsInUse
		avgPC = s.computedProxiedConcurrency / s.secondsInUse
	}
	return avgC, avgPC, s.requestCount, s.proxiedCount
}

// reset resets the state so a new reporting cycle can start.
func (s *RequestStats) reset() {
	s.computedConcurrency, s.computedProxiedConcurrency = 0, 0
	s.requestCount, s.proxiedCount = 0, 0
	s.secondsInUse = 0
}
