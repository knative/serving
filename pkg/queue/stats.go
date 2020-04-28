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

	"knative.dev/serving/pkg/network"
)

// NewStats instantiates a new instance of Stats.
func NewStats(startedAt time.Time, reqCh chan network.ReqEvent, reportCh <-chan time.Time, report func(float64, float64, float64, float64)) {
	go func() {
		state := network.NewRequestStats(startedAt)

		for {
			select {
			case event := <-reqCh:
				state.HandleEvent(event)
			case now := <-reportCh:
				stats := state.Report(now)
				report(
					stats.AverageConcurrency,
					stats.AverageProxiedConcurrency,
					stats.RequestCount,
					stats.ProxiedRequestCount,
				)
			}
		}
	}()
}
