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

package reconciler

import (
	"context"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"time"
)

type Measurement int

// ServiceTimeUntilReadyM is the time it takes for a service to become ready since the resource is created
const ServiceTimeUntilReadyM Measurement = iota

var measurements = []*stats.Float64Measure{
	ServiceTimeUntilReadyM: stats.Float64(
		"service_time_until_ready",
		"Time it takes for a service to become ready since created",
		stats.UnitMilliseconds),
}


func init() {
	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	err := view.Register(
		&view.View{
			Description: measurements[ServiceTimeUntilReadyM].Description(),
			Measure: measurements[ServiceTimeUntilReadyM],
			Aggregation: view.LastValue(),
		},
	)
	if err != nil {
		panic(err)
	}
}

// StatsReporter reports reconcilers' metrics
type StatsReporter struct {
	ctx context.Context
}

// NewStatsReporter creates a reporter for reconcilers' metrics
func NewStatsReporter() *StatsReporter {
	return &StatsReporter{
		ctx: context.Background(),
	}
}

func (r *StatsReporter) Report(m Measurement, v float64) {
	stats.Record(r.ctx, measurements[m].M(v))
}

func (r *StatsReporter) ReportDuration(m Measurement, d time.Duration) {
	r.Report(m, float64(d / time.Millisecond))
}
