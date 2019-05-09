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

package activator

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/knative/pkg/metrics"
	"github.com/knative/pkg/metrics/metricskey"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	requestCountM = stats.Int64(
		"request_count",
		"The number of requests that are routed to Activator",
		stats.UnitDimensionless)
	responseTimeInMsecM = stats.Float64(
		"request_latencies",
		"The response time in millisecond",
		stats.UnitMilliseconds)

	defaultLatencyDistribution = view.Distribution(0, 5, 10, 20, 40, 60, 80, 100, 150, 200, 250, 300, 350, 400, 450, 500, 600, 700, 800, 900, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
)

// StatsReporter defines the interface for sending activator metrics
type StatsReporter interface {
	ReportRequestCount(ns, service, config, rev string, responseCode, numTries int, v int64) error
	ReportResponseTime(ns, service, config, rev string, responseCode int, d time.Duration) error
}

// Reporter holds cached metric objects to report autoscaler metrics
type Reporter struct {
	initialized          bool
	namespaceTagKey      tag.Key
	serviceTagKey        tag.Key
	configTagKey         tag.Key
	revisionTagKey       tag.Key
	responseCodeKey      tag.Key
	responseCodeClassKey tag.Key
	numTriesKey          tag.Key
}

// NewStatsReporter creates a reporter that collects and reports activator metrics
func NewStatsReporter() (*Reporter, error) {
	var r = &Reporter{}

	// Create the tag keys that will be used to add tags to our measurements.
	nsTag, err := tag.NewKey(metricskey.LabelNamespaceName)
	if err != nil {
		return nil, err
	}
	r.namespaceTagKey = nsTag
	serviceTag, err := tag.NewKey(metricskey.LabelServiceName)
	if err != nil {
		return nil, err
	}
	r.serviceTagKey = serviceTag
	configTag, err := tag.NewKey(metricskey.LabelConfigurationName)
	if err != nil {
		return nil, err
	}
	r.configTagKey = configTag
	revTag, err := tag.NewKey(metricskey.LabelRevisionName)
	if err != nil {
		return nil, err
	}
	r.revisionTagKey = revTag
	responseCodeTag, err := tag.NewKey("response_code")
	if err != nil {
		return nil, err
	}
	r.responseCodeKey = responseCodeTag
	responseCodeClassTag, err := tag.NewKey("response_code_class")
	if err != nil {
		return nil, err
	}
	r.responseCodeClassKey = responseCodeClassTag
	numTriesTag, err := tag.NewKey("num_tries")
	if err != nil {
		return nil, err
	}
	r.numTriesKey = numTriesTag
	// Create view to see our measurements.
	err = view.Register(
		&view.View{
			Description: "The number of requests that are routed to Activator",
			Measure:     requestCountM,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.serviceTagKey, r.configTagKey, r.revisionTagKey, r.responseCodeKey, r.responseCodeClassKey, r.numTriesKey},
		},
		&view.View{
			Description: "The response time in millisecond",
			Measure:     responseTimeInMsecM,
			Aggregation: defaultLatencyDistribution,
			TagKeys:     []tag.Key{r.namespaceTagKey, r.serviceTagKey, r.configTagKey, r.revisionTagKey, r.responseCodeClassKey, r.responseCodeKey},
		},
	)
	if err != nil {
		return nil, err
	}

	r.initialized = true
	return r, nil
}

func valueOrUnknown(v string) string {
	if v != "" {
		return v
	}
	return metricskey.ValueUnknown
}

// ReportRequestCount captures request count metric with value v.
func (r *Reporter) ReportRequestCount(ns, service, config, rev string, responseCode, numTries int, v int64) error {
	if !r.initialized {
		return errors.New("StatsReporter is not initialized yet")
	}

	// Note that service names can be an empty string, so it needs a special treatment.
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(r.namespaceTagKey, ns),
		tag.Insert(r.serviceTagKey, valueOrUnknown(service)),
		tag.Insert(r.configTagKey, config),
		tag.Insert(r.revisionTagKey, rev),
		tag.Insert(r.responseCodeKey, strconv.Itoa(responseCode)),
		tag.Insert(r.responseCodeClassKey, responseCodeClass(responseCode)),
		tag.Insert(r.numTriesKey, strconv.Itoa(numTries)))
	if err != nil {
		return err
	}

	metrics.Record(ctx, requestCountM.M(v))
	return nil
}

// ReportResponseTime captures response time requests
func (r *Reporter) ReportResponseTime(ns, service, config, rev string, responseCode int, d time.Duration) error {
	if !r.initialized {
		return errors.New("StatsReporter is not initialized yet")
	}

	// Note that service names can be an empty string, so it needs a special treatment.
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(r.namespaceTagKey, ns),
		tag.Insert(r.serviceTagKey, valueOrUnknown(service)),
		tag.Insert(r.configTagKey, config),
		tag.Insert(r.revisionTagKey, rev),
		tag.Insert(r.responseCodeKey, strconv.Itoa(responseCode)),
		tag.Insert(r.responseCodeClassKey, responseCodeClass(responseCode)))
	if err != nil {
		return err
	}

	// convert time.Duration in nanoseconds to milliseconds
	metrics.Record(ctx, responseTimeInMsecM.M(float64(d/time.Millisecond)))
	return nil
}

// responseCodeClass converts response code to a string of response code class.
// e.g. The response code class is "5xx" for response code 503.
func responseCodeClass(responseCode int) string {
	// Get the hundred digit of the response code and concatenate "xx".
	return strconv.Itoa(responseCode/100) + "xx"
}
