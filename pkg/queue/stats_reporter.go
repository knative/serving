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

// Measurement type for reporting.
type Measurement int

const (
	// ReportingPeriod interval of time for reporting.
	ReportingPeriod = 10 * time.Second

	// OperationsPerSecondN
	OperationsPerSecondN = "operations_per_second"
	// AverageConcurrentRequestsN
	AverageConcurrentRequestsN = "average_concurrent_requests"
	// LameDuckN
	LameDuckN = "lame_duck"

	// OperationsPerSecondM number of operations per second.
	OperationsPerSecondM Measurement = iota
	// AverageConcurrentRequestsM average number of requests currently being handled by this pod.
	AverageConcurrentRequestsM
	// LameDuckM indicates this Pod has received a shutdown signal.
	LameDuckM
)

var (
	measurements = []*stats.Float64Measure{
		// TODO(#2524): make reporting period accurate.
		OperationsPerSecondM: stats.Float64(
			OperationsPerSecondN,
			"Number of operations per second",
			stats.UnitNone),
		AverageConcurrentRequestsM: stats.Float64(
			AverageConcurrentRequestsN,
			"Number of requests currently being handled by this pod",
			stats.UnitNone),
		LameDuckM: stats.Float64(
			LameDuckN,
			"Indicates this Pod has received a shutdown signal with 1 else 0",
			stats.UnitNone),
	}
)

// Reporter structure representing a prometheus expoerter.
type Reporter struct {
	Initialized     bool
	ctx             context.Context
	configTagKey    tag.Key
	namespaceTagKey tag.Key
	revisionTagKey  tag.Key
	podTagKey       tag.Key
}

// NewStatsReporter creates a reporter that collects and reports queue metrics
func NewStatsReporter(namespace string, config string, revision string, pod string) (*Reporter, error) {
	var r = &Reporter{}
	if len(namespace) < 1 {
		return nil, errors.New("Namespace must not be empty")
	}
	if len(config) < 1 {
		return nil, errors.New("Config must not be empty")
	}
	if len(revision) < 1 {
		return nil, errors.New("Revision must not be empty")
	}
	if len(pod) < 1 {
		return nil, errors.New("Pod must not be empty")
	}

	// Create the tag keys that will be used to add tags to our measurements.
	nsTag, err := tag.NewKey("destination_namespace")
	if err != nil {
		return nil, err
	}
	r.namespaceTagKey = nsTag
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
	podTag, err := tag.NewKey("destination_pod")
	if err != nil {
		return nil, err
	}
	r.podTagKey = podTag

	// Create views to see our measurements. This can return an error if
	// a previously-registered view has the same name with a different value.
	// View name defaults to the measure name if unspecified.
	err = view.Register(
		&view.View{
			Description: "Number of requests received since last Stat",
			Measure:     measurements[OperationsPerSecondM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.configTagKey, r.revisionTagKey, r.podTagKey},
		},
		&view.View{
			Description: "Number of requests currently being handled by this pod",
			Measure:     measurements[AverageConcurrentRequestsM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.configTagKey, r.revisionTagKey, r.podTagKey},
		},
		&view.View{
			Description: "Indicates this Pod has received a shutdown signal with 1 else 0",
			Measure:     measurements[LameDuckM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.configTagKey, r.revisionTagKey, r.podTagKey},
		},
	)
	if err != nil {
		return nil, err
	}

	ctx, err := tag.New(
		context.Background(),
		tag.Insert(r.namespaceTagKey, namespace),
		tag.Insert(r.configTagKey, config),
		tag.Insert(r.revisionTagKey, revision),
		tag.Insert(r.podTagKey, pod),
	)
	if err != nil {
		return nil, err
	}
	r.ctx = ctx
	r.Initialized = true
	return r, nil
}

// Report captures request metrics
func (r *Reporter) Report(lameDuck bool, operationsPerSecond float64, averageConcurrentRequests float64) error {
	if !r.Initialized {
		return errors.New("StatsReporter is not Initialized yet")
	}
	_lameDuck := float64(0)
	if lameDuck {
		_lameDuck = float64(1)
	}
	stats.Record(r.ctx, measurements[LameDuckM].M(_lameDuck))
	stats.Record(r.ctx, measurements[OperationsPerSecondM].M(operationsPerSecond))
	stats.Record(r.ctx, measurements[AverageConcurrentRequestsM].M(averageConcurrentRequests))
	return nil
}

// UnregisterViews Unregister views
func (r *Reporter) UnregisterViews() error {
	if r.Initialized != true {
		return errors.New("Reporter is not initialized")
	}
	var views []*view.View
	if v := view.Find(OperationsPerSecondN); v != nil {
		views = append(views, v)
	}
	if v := view.Find(AverageConcurrentRequestsN); v != nil {
		views = append(views, v)
	}
	if v := view.Find(LameDuckN); v != nil {
		views = append(views, v)
	}
	view.Unregister(views...)
	r.Initialized = false
	return nil
}
