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

// ReportStats continually processes network events from reqCh and reports
// aggregated stats via the `report` function whenever reportCh ticks.
func ReportStats(startedAt time.Time, stats *network.RequestStats, reportCh <-chan time.Time, report func(float64, float64, float64, float64)) {
	for {
		select {
		case now := <-reportCh:
			stat := stats.Report(now)
			report(
				stat.AverageConcurrency,
				stat.AverageProxiedConcurrency,
				stat.RequestCount,
				stat.ProxiedRequestCount,
			)
		}
	}
}
