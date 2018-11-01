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

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

type Measurement int

// TODO(@mrmcmuffinz): Need to move this to a more appropriate place.
// TODO(@mrmcmuffinz): Need to get more appropriate names.
const (
	// ReportingPeriod interval of time for reporting.
	ReportingPeriod = 10 * time.Second
	// RequestCountM number of requests received since last Stat (approximately QPS).
	RequestCountM Measurement = iota
	// AverageConcurrentRequestsM average number of requests currently being handled by this pod.
	AverageConcurrentRequestsM
	// LameDuckM indicates this Pod has received a shutdown signal.
	LameDuckM
)

// TODO(@mrmcmuffinz): Need to move this to a more appropriate place.
var (
	measurements = []*stats.Float64Measure{
		RequestCountM: stats.Float64(
			"request_count",
			"Number of requests received since last Stat",
			stats.UnitNone),
		AverageConcurrentRequestsM: stats.Float64(
			"average_concurrent_requests",
			"Number of requests currently being handled by this pod",
			stats.UnitNone),
		LameDuckM: stats.Float64(
			"lame_duck",
			"Indicates this Pod has received a shutdown signal with 1 else 0",
			stats.UnitNone),
	}
	namespaceTagKey tag.Key
	configTagKey    tag.Key
	revisionTagKey  tag.Key
	serviceTagKey   tag.Key
)

func init() {
	var err error
	// Create the tag keys that will be used to add tags to our measurements.
	// Tag keys must conform to the restrictions described in
	// go.opencensus.io/tag/validate.go. Currently those restrictions are:
	// - length between 1 and 255 inclusive
	// - characters are printable US-ASCII
	namespaceTagKey, err = tag.NewKey("configuration_namespace")
	if err != nil {
		panic(err)
	}
	serviceTagKey, err = tag.NewKey("service")
	if err != nil {
		panic(err)
	}
	configTagKey, err = tag.NewKey("configuration")
	if err != nil {
		panic(err)
	}
	revisionTagKey, err = tag.NewKey("revision")
	if err != nil {
		panic(err)
	}

	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	err = view.Register(
		&view.View{
			Description: "Number of requests received since last Stat",
			Measure:     measurements[RequestCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Number of requests currently being handled by this pod",
			Measure:     measurements[AverageConcurrentRequestsM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Indicates this Pod has received a shutdown signal with 1 else 0",
			Measure:     measurements[LameDuckM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
	)
	if err != nil {
		panic(err)
	}
}

/*
// TODO(@mrmcmuffinz): Need to figure out how to properly do the reporting.
// The below code was borrowed from stats_reporter.go in activator cmd
// command component but not sure yet what all applies.

// StatsReporter defines the interface for sending autoscaler metrics
type StatsReporter interface {
	Report(m Measurement, v float64) error
}

// Reporter holds cached metric objects to report autoscaler metrics
type Reporter struct {
	ctx         context.Context
	logger      *zap.SugaredLogger
	initialized bool
}

// NewStatsReporter creates a reporter that collects and reports autoscaler metrics
func NewStatsReporter(podNamespace string, service string, config string, revision string) (*Reporter, error) {

	r := &Reporter{}

	// Our tags are static. So, we can get away with creating a single context
	// and reuse it for reporting all of our metrics.
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(namespaceTagKey, podNamespace),
		tag.Insert(serviceTagKey, service),
		tag.Insert(configTagKey, config),
		tag.Insert(revisionTagKey, revision))
	if err != nil {
		return nil, err
	}

	r.ctx = ctx
	r.initialized = true
	return r, nil
}

// Report captures value v for measurement m
func (r *Reporter) Report(m Measurement, v float64) error {
	if !r.initialized {
		return errors.New("StatsReporter is not initialized yet")
	}

	stats.Record(r.ctx, measurements[m].M(v))
	return nil
}
*/
