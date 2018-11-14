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
	"context"
	"errors"
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
)

// Reporter structure representing a prometheus expoerter.
type Reporter struct {
	initialized     bool
	configTagKey    tag.Key
	namespaceTagKey tag.Key
	numTriesKey     tag.Key
	revisionTagKey  tag.Key
	responseCodeKey tag.Key
	serviceTagKey   tag.Key
}

// NewStatsReporter creates a reporter that collects and reports queue metrics
func NewStatsReporter() (*Reporter, error) {
	var r = &Reporter{}

	// Create the tag keys that will be used to add tags to our measurements.
	nsTag, err := tag.NewKey("destination_namespace")
	if err != nil {
		return nil, err
	}
	r.namespaceTagKey = nsTag
	serviceTag, err := tag.NewKey("destination_service")
	if err != nil {
		return nil, err
	}
	r.serviceTagKey = serviceTag
	configTag, err := tag.NewKey("destination_configuration")
	if err != nil {
		return nil, err
	}
	r.configTagKey = configTag
	revTag, err := tag.NewKey("destination_revision")
	if err != nil {
		return nil, err
	}
	r.revisionTagKey = revTag
	responseCodeTag, err := tag.NewKey("response_code")
	if err != nil {
		return nil, err
	}
	r.responseCodeKey = responseCodeTag
	numTriesTag, err := tag.NewKey("num_tries")
	if err != nil {
		return nil, err
	}
	r.numTriesKey = numTriesTag

	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	err = view.Register(
		&view.View{
			Description: "Number of requests received since last Stat",
			Measure:     measurements[RequestCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.serviceTagKey, r.configTagKey, r.revisionTagKey},
		},
		&view.View{
			Description: "Number of requests currently being handled by this pod",
			Measure:     measurements[AverageConcurrentRequestsM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.serviceTagKey, r.configTagKey, r.revisionTagKey},
		},
		&view.View{
			Description: "Indicates this Pod has received a shutdown signal with 1 else 0",
			Measure:     measurements[LameDuckM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.serviceTagKey, r.configTagKey, r.revisionTagKey},
		},
	)
	if err != nil {
		return nil, err
	}

	r.initialized = true
	return r, nil
}

// Report captures request metrics
func (r *Reporter) Report(lameDuck float64, requestCount float64, averageConcurrentRequests float64) error {
	if !r.initialized {
		return errors.New("StatsReporter is not initialized yet")
	}

	ctx, err := tag.New(context.Background())
	/*
		tag.Insert(r.namespaceTagKey, ns),
		tag.Insert(r.serviceTagKey, service),
		tag.Insert(r.configTagKey, config),
		tag.Insert(r.revisionTagKey, rev))
	*/
	if err != nil {
		return err
	}

	stats.Record(ctx, measurements[LameDuckM].M(lameDuck))
	stats.Record(ctx, measurements[RequestCountM].M(requestCount))
	stats.Record(ctx, measurements[AverageConcurrentRequestsM].M(averageConcurrentRequests))
	return nil
}
