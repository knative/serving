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
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

type Measurement int

// ServiceTimeUntilReadyM is the time it takes for a service to become ready since the resource is created
const ServiceTimeUntilReadyM Measurement = iota

var (
	measurements = []*stats.Float64Measure{
		ServiceTimeUntilReadyM: stats.Float64(
			"service_time_until_ready",
			"Time it takes for a service to become ready since created",
			stats.UnitMilliseconds),
	}
	reconcilerTagKey tag.Key
)


func init() {
	var err error
	// Create the tag keys that will be used to add tags to our measurements.
	// Tag keys must conform to the restrictions described in
	// go.opencensus.io/tag/validate.go. Currently those restrictions are:
	// - length between 1 and 255 inclusive
	// - characters are printable US-ASCII
	reconcilerTagKey, err = tag.NewKey("reconciler")
	if err != nil {
		panic(err)
	}

	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	err = view.Register(
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
type StatsReporter interface {
	// Report captures value v for measurement v
	Report(m Measurement, v float64) error
	// Report captures duration d for measurement v
	ReportDuration(m Measurement, d time.Duration) error
}

type reporter struct {
	reconciler string
	ctx context.Context
}

// NewStatsReporter creates a reporter for reconcilers' metrics
func NewStatsReporter(reconciler string) StatsReporter {
	return &reporter{
		reconciler: reconciler,
		ctx: context.Background(),
	}
}

// Report captures value v for measurement v
func (r *reporter) Report(m Measurement, v float64) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(reconcilerTagKey, r.reconciler))
	if err != nil {
		return err
	}

	stats.Record(ctx, measurements[m].M(v))
	return nil
}

// Report captures duration d for measurement v
func (r *reporter) ReportDuration(m Measurement, d time.Duration) error {
	return r.Report(m, float64(d / time.Millisecond))
}
