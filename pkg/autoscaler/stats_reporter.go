/*
Copyright 2018 Google Inc. All Rights Reserved.
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
)

// Measurement represents the type of the autoscaler metric to be reported
type Measurement int

const (
	// DesiredPodCountM is used for the pod count that autoscaler wants
	DesiredPodCountM Measurement = 0
	// RequestedPodCountM is used for the requested pod count from kubernetes
	RequestedPodCountM Measurement = 1
	// ActualPodCountM is used for the actual number of pods we have
	ActualPodCountM Measurement = 2
	// PanicM is used as a flag to indicate if autoscaler is in panic mode or not
	PanicM        Measurement = 3
	lastEnumEntry             = (int)(PanicM)
)

// StatsReporter defines the interface for sending autoscaler metrics
type StatsReporter interface {
	Report(m Measurement, v int64) error
}

// Reporter holds cached metrics objects to report autoscaler metrics
type Reporter struct {
	measurements [lastEnumEntry + 1]*stats.Int64Measure
	ctx          context.Context
	initialized  bool
}

// NewStatsReporter creates a reporter that collects and reports autoscaler metrics
func NewStatsReporter(podNamespace string, config string, revision string) (*Reporter, error) {

	var r = &Reporter{}
	r.measurements[DesiredPodCountM] = stats.Int64("desired_pod_count", "Number of pods autoscaler wants to allocate", stats.UnitNone)
	r.measurements[RequestedPodCountM] = stats.Int64("requested_pod_count", "Number of pods autoscaler requested from Kubernetes", stats.UnitNone)
	r.measurements[ActualPodCountM] = stats.Int64("actual_pod_count", "Number of pods that are allocated currently", stats.UnitNone)
	r.measurements[PanicM] = stats.Int64("panic_mode", "1 if autoscaler is in panic mode, 0 otherwise", stats.UnitNone)

	// Create the tag keys that will be used to add tags to our measurements.
	namespaceTagKey, err := tag.NewKey("configuration_namespace")
	if err != nil {
		return nil, err
	}

	configTagKey, err := tag.NewKey("configuration")
	if err != nil {
		return nil, err
	}

	revisionTagKey, err := tag.NewKey("revision")
	if err != nil {
		return nil, err
	}

	// Create view to see our measurements.
	err = view.Register(
		&view.View{
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     r.measurements[DesiredPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Number of pods autoscaler requested from Kubernetes",
			Measure:     r.measurements[RequestedPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "Number of pods that are allocated currently",
			Measure:     r.measurements[ActualPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, configTagKey, revisionTagKey},
		},
		&view.View{
			Description: "1 if autoscaler is in panic mode, 0 otherwise",
			Measure:     r.measurements[PanicM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, configTagKey, revisionTagKey},
		},
	)
	if err != nil {
		return nil, err
	}

	// Our tags are static. So, we can get away with creating a single context
	// and reuse it for reporting all of our metrics.
	r.ctx, err = tag.New(
		context.Background(),
		tag.Insert(namespaceTagKey, podNamespace),
		tag.Insert(configTagKey, config),
		tag.Insert(revisionTagKey, revision))
	if err != nil {
		return nil, err
	}

	r.initialized = true
	return r, nil
}

// Report captures value v for measurement m
func (r *Reporter) Report(m Measurement, v int64) error {
	if !r.initialized {
		return errors.New("StatsReporter is not initialized yet")
	}

	stats.Record(r.ctx, r.measurements[m].M(v))
	return nil
}
