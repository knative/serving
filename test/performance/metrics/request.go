/*
Copyright 2019 The Knative Authors

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

package metrics

import (
	"github.com/google/mako/go/quickstore"
	vegeta "github.com/tsenart/vegeta/lib"

	"knative.dev/pkg/test/mako"
)

// AggregateResult is the aggregated result of requests for better visualization.
type AggregateResult struct {
	// ErrorRates is a map that saves the number of errors for each timestamp (in secs)
	ErrorRates map[int64]int64
	// RequestRates is a map that saves the number of requests for each timestamp (in secs)
	RequestRates map[int64]int64
}

// NewAggregateResult returns the pointer of a new AggregateResult object.
func NewAggregateResult(initialSize int) *AggregateResult {
	return &AggregateResult{
		ErrorRates:   make(map[int64]int64, initialSize),
		RequestRates: make(map[int64]int64, initialSize),
	}
}

// HandleResult will handle the attack result by:
// 1. Adding its latency as a sample point if no error, or adding it as an error if there is
// 2. Updating the aggregate results
func HandleResult(q *quickstore.Quickstore, res vegeta.Result, latencyKey string, ar *AggregateResult) {
	// Handle the result by reporting an error or a latency sample point.
	var isAnError int64
	if res.Error != "" {
		// By reporting errors like this the error strings show up on
		// the details page for each Mako run.
		q.AddError(mako.XTime(res.Timestamp), res.Error)
		isAnError = 1
	} else {
		// Add a sample points for the target benchmark's latency stat
		// with the latency of the request this result is for.
		q.AddSamplePoint(mako.XTime(res.Timestamp), map[string]float64{
			latencyKey: res.Latency.Seconds(),
		})
		isAnError = 0
	}

	// Update our error and request rates.
	// We handle errors this way to force zero values into every time for
	// which we have data, even if there is no error.
	ar.ErrorRates[res.Timestamp.Unix()] += isAnError
	ar.RequestRates[res.Timestamp.Unix()]++
}
