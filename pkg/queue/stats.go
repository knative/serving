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
	"time"
)

// ReqEvent represents either an incoming or closed request.
type ReqEvent struct {
	Time      time.Time
	EventType ReqEventType
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

// NewStats instantiates a new instance of Stats.
func NewStats(startedAt time.Time, reqCh chan ReqEvent, reportInterval time.Duration,
	report func(float64, float64, float64, float64)) {
	newStatsWithTicker(startedAt, reqCh, reportInterval, time.NewTicker(reportInterval).C, report)
}

func newStatsWithTicker(startedAt time.Time, reqCh chan ReqEvent, reportInterval time.Duration,
	ticker <-chan time.Time, report func(float64, float64, float64, float64)) {

	go func() {
		var (
			intervalSecs = reportInterval.Seconds()

			// State variables that track the current state. Not resettet after reporting.
			concurrency, proxiedConcurrency float64
			lastChange                      = startedAt

			// Reporting variables that track state over the current window. Resettet after
			// reporting.
			requestCount, proxiedCount, averageConcurrency, averageProxiedConcurrency float64
		)

		// Updates the lastChanged/timeOnConcurrency state
		// Note: due to nature of the channels used below, the ReportChan
		// can race the ReqChan, thus an event can arrive that has a lower
		// timestamp than `lastChange`. This is ignored, since it only makes
		// for very slight differences.
		updateState := func(time time.Time) {
			if durationSinceChange := time.Sub(lastChange); durationSinceChange > 0 {
				durationSecs := durationSinceChange.Seconds()
				averageConcurrency += concurrency * durationSecs / intervalSecs
				averageProxiedConcurrency += proxiedConcurrency * durationSecs / intervalSecs
				lastChange = time
			}
		}

		for {
			select {
			case event := <-reqCh:
				updateState(event.Time)

				switch event.EventType {
				case ProxiedIn:
					proxiedConcurrency++
					proxiedCount++
					fallthrough
				case ReqIn:
					requestCount++
					concurrency++
				case ProxiedOut:
					proxiedConcurrency--
					fallthrough
				case ReqOut:
					concurrency--
				}
			case now := <-ticker:
				updateState(now)

				report(averageConcurrency, averageProxiedConcurrency, requestCount, proxiedCount)

				// Reset the stat counts which have been reported.
				averageConcurrency, averageProxiedConcurrency = 0, 0
				requestCount, proxiedCount = 0, 0
			}
		}
	}()
}
