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

package autoscaler

import (
	"context"
	"errors"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/zap"
)

// Measurement represents the type of the autoscaler metric to be reported
type Measurement int

const (
	// DesiredPodCountM is used for the pod count that autoscaler wants
	DesiredPodCountM Measurement = iota
	// RequestedPodCountM is used for the requested pod count from kubernetes
	RequestedPodCountM
	// ActualPodCountM is used for the actual number of pods we have
	ActualPodCountM
	// ObservedPodCountM is used for the observed number of pods we have
	ObservedPodCountM
	// ObservedStableConcurrencyM is the average of requests count in each 60 second stable window
	ObservedStableConcurrencyM
	// ObservedPanicConcurrencyM is the average of requests count in each 6 second panic window
	ObservedPanicConcurrencyM
	// TargetConcurrencyM is the desired number of concurrent requests for each pod
	TargetConcurrencyM
	// PanicM is used as a flag to indicate if autoscaler is in panic mode or not
	PanicM
)

var (
	measurements = []*stats.Float64Measure{
		DesiredPodCountM: stats.Float64(
			"desired_pod_count",
			"Number of pods autoscaler wants to allocate",
			stats.UnitNone),
		RequestedPodCountM: stats.Float64(
			"requested_pod_count",
			"Number of pods autoscaler requested from Kubernetes",
			stats.UnitNone),
		ActualPodCountM: stats.Float64(
			"actual_pod_count",
			"Number of pods that are allocated currently",
			stats.UnitNone),
		ObservedPodCountM: stats.Float64(
			"observed_pod_count",
			"Number of pods that are observed currently",
			stats.UnitNone),
		ObservedStableConcurrencyM: stats.Float64(
			"observed_stable_concurrency",
			"Average of requests count in each 60 second stable window",
			stats.UnitNone),
		ObservedPanicConcurrencyM: stats.Float64(
			"observed_panic_concurrency",
			"Average of requests count in each 6 second panic window",
			stats.UnitNone),
		TargetConcurrencyM: stats.Float64(
			"target_concurrency_per_pod",
			"The desired number of concurrent requests for each pod",
			stats.UnitNone),
		PanicM: stats.Float64(
			"panic_mode",
			"1 if autoscaler is in panic mode, 0 otherwise",
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
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     measurements[DesiredPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Number of pods autoscaler requested from Kubernetes",
			Measure:     measurements[RequestedPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Number of pods that are allocated currently",
			Measure:     measurements[ActualPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Number of pods that are observed currently",
			Measure:     measurements[ObservedPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Average of requests count in each 60 second stable window",
			Measure:     measurements[ObservedStableConcurrencyM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Average of requests count in each 6 second panic window",
			Measure:     measurements[ObservedPanicConcurrencyM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "The desired number of concurrent requests for each pod",
			Measure:     measurements[TargetConcurrencyM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "1 if autoscaler is in panic mode, 0 otherwise",
			Measure:     measurements[PanicM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, serviceTagKey, configTagKey, revisionTagKey},
		},
	)
	if err != nil {
		panic(err)
	}

}

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
