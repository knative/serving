// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// see the license for the specific language governing permissions and
// limitations under the license.

// Package quickstore offers a way to utilize Mako storage, downsampling,
// aggregation and analyzers in a simple way. This is most helpful when you have
// a pre-existing benchmarking tool which exports data that you would like to
// save in Mako.
//
// To use: Understand and create a Mako benchmark to hold your data. See
//   go/mako-help?tmpl=storage#bench for more information about benchmarks.
//   Watch 'Chart' videos at go/mako-videos for demos of the Mako
//   dashboard.
//
// Basic usage:
//  // Store data, metric and run aggregates will be calculated automatically.
//	q := quickstore.Quickstore{BenchmarkKey: myBenchmarkKey}
//  for _, data := range []float64{4, 6, 40, 3.2} {
//    q.AddSamplePoint(timestampMs, data)
//  output, err = q.Store()
//  if err != nil {
//		if output.GetStatus() == qpb.QuickstoreOutput_ANALYSIS_FAIL {
//			// handle analysis failure
//		} else {
//			// handle error
//		}
//  }
//
// See more examples inside https://github.com/google/mako/blob/master/go/quickstore/quickstore_example_test.go
//
// Struct is not concurrent safe
//
// More information about quickstore: go/mako-quickstore
package quickstore

import (
	"context"
	"errors"

	"flag"
	"os"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	qspb "github.com/google/mako/internal/quickstore_microservice/proto/quickstore_go_proto"
	qpb "github.com/google/mako/proto/quickstore/quickstore_go_proto"
	pgpb "github.com/google/mako/spec/proto/mako_go_proto"

	_ "github.com/google/mako/internal/go/common" // b/111726961
)

// Quickstore allows saving of data passed via Add* methods to Mako.
// Zero struct is usable but before calling Store() you must populate:
//   * BenchmarkKey to the Mako benchmark you'd like your data saved to.
//   OR
//   * Input (with Input.BenchmarkKey set) to define more information about the
//     Mako run where you data will be stored. See QuickstoreInput for more
//     information.
type Quickstore struct {
	// BenchmarkKey to where your data will be stored to.
	BenchmarkKey string
	// Allows more information to be passed about the Mako Run that will be
	// created.
	Input              qpb.QuickstoreInput
	samplePoints       []*pgpb.SamplePoint
	sampleErrors       []*pgpb.SampleError
	runAggregates      []*pgpb.KeyedValue
	metricAggValueKeys []string
	metricAggTypes     []string
	metricAggValues    []float64
	saverImpl          saver
}

// NewAtAddress creates a new Quickstore that connects to a Quickstore microservice at the provided gRPC address.
//
// Along with the Quickstore instance, it returns a function that can be called to request
// the microsevice terminate itself. This function can be ignored in order to leave the microservice
// running.
func NewAtAddress(ctx context.Context, input *qpb.QuickstoreInput, address string) (*Quickstore, func(context.Context), error) {
	conn, err := grpc.DialContext(ctx, address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}

	client := qspb.NewQuickstoreClient(conn)
	return &Quickstore{
			Input:     *input,
			saverImpl: &grpcSaver{client},
		}, func(ctx context.Context) {
			client.ShutdownMicroservice(ctx, &qspb.ShutdownInput{})
		}, nil
}

// AddSamplePoint adds a sample at the specified x-value.
//
// How the x-value is interpreted depends on your benchmark configuration. See
// BenchmarkInfo.input_value_info for more information.
//
// The map represents a mapping from metric to value.
// It is more efficient for Mako to store multiple metrics collected at
// the same xval together, but it is optional.
//
// When adding data via this function, calling the Add*Aggregate() functions
// is optional, as the aggregates will get computed by this class.
//
// An error is returned if there was a problem adding data.
func (q *Quickstore) AddSamplePoint(xval float64, valueKeyToYVals map[string]float64) error {
	s := pgpb.SamplePoint{InputValue: proto.Float64(xval)}
	for valueKey, value := range valueKeyToYVals {
		s.MetricValueList = append(s.MetricValueList,
			&pgpb.KeyedValue{
				ValueKey: proto.String(valueKey),
				Value:    proto.Float64(value)})
	}
	q.samplePoints = append(q.samplePoints, &s)
	return nil
}

// AddError add an error at the specified xval.
//
// When adding errors via this function, the aggregate error count will be set
// automatically.
//
// An error is returned if there was a problem adding data.
func (q *Quickstore) AddError(xval float64, errorMessage string) error {
	q.sampleErrors = append(q.sampleErrors, &pgpb.SampleError{InputValue: proto.Float64(xval),
		ErrorMessage: proto.String(errorMessage)})
	return nil
}

// AddRunAggregate adds an aggregate value over the entire run.
// If value_key is:
//  * "~ignore_sample_count"
//  * "~usable_sample_count"
//  * "~error_sample_count"
//  * "~benchmark_score"
//  The corresponding value will be overwritten inside
//  https://github.com/google/mako/blob/master/spec/proto/mako.proto
//  of these values are provided, they will be calculated automatically by the
//  framework based on SamplePoints/Errors provided before Store() is called.
//
// Otherwise the value_key will be set to a custom aggregate (See
// https://github.com/google/mako/blob/master/spec/proto/mako.proto
//
// If no run aggregates are manully set with this method, values are
// automatically calculated.
//
// An error is returned if there was a problem adding data.
func (q *Quickstore) AddRunAggregate(valueKey string, value float64) error {
	q.runAggregates = append(q.runAggregates, &pgpb.KeyedValue{ValueKey: proto.String(valueKey),
		Value: proto.Float64(value)})
	return nil
}

// AddMetricAggregate adds an aggregate for a specific metric.
// If value_key is:
//  * "min"
//  * "max"
//  * "mean"
//  * "median"
//  * "standard_deviation"
//  * "median_absolute_deviation"
//  * "count"
//  The corresponding value inside
//  https://github.com/google/mako/blob/master/spec/proto/mako.proto
//  be set.
//
// The value_key can also represent a percentile see
// https://github.com/google/mako/blob/master/spec/proto/mako.proto
//
// For example "p98000" would be interpreted as the 98th percentile. These
// need to correspond to the percentiles that your benchmark has set.
// It is an error to supply an percentile that is not part of your benchmark.
// If any percentiles are provided, the automatically calculated percentiles
// will be cleared to 0.
//
// If any aggregate_types (eg. "min") are set for a value_key it will
// overwrite the entire MetricAggregate for that value_key. If no
// aggregate_types are provided for a value_key metric aggregates (including
// percentiles) will be calculated automatically based on data provided via
// calls to AddSamplePoint.
//
// An error is returned if there was a problem adding data.
func (q *Quickstore) AddMetricAggregate(valueKey string, aggregateType string, value float64) error {
	q.metricAggValueKeys = append(q.metricAggValueKeys, valueKey)
	q.metricAggTypes = append(q.metricAggTypes, aggregateType)
	q.metricAggValues = append(q.metricAggValues, value)
	return nil
}

// Store all the values that you have added. You cannot save if no Add*()
// functions have been called.
//
// Each call to Store() will create a new unique Mako Run and store all
// Aggregate and SamplePoint data registered using the Add* methods since the
// last call to Store() as a part of that new Run.
//
// Data can be added via Add* calls in any order.
//
// An error is returned if the Store call encountered an error. See the
// QuickstoreOutput proto buffer also returned for more information about the
// failure.
func (q *Quickstore) Store() (qpb.QuickstoreOutput, error) {
	// Workaround for b/137108136 to make errors in Go logs about flags not being parsed go away.
	if !flag.Parsed() {
		// It's possible users are using some other flag library and/or are purposely not calling flag.Parse() for some other reason which would result in a call to flag.Parse() failing.
		// Instead of exiting, set things up so that we can just log the error and continue.
		flag.CommandLine.Init(os.Args[0], flag.ContinueOnError)
		if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
			log.Errorf("Error parsing flags: %v", err)
		}
	}
	save := q.saverImpl

	if q.BenchmarkKey != "" {
		q.Input.BenchmarkKey = proto.String(q.BenchmarkKey)
	}

	log.Info("Attempting to store:")
	log.Infof("%d SamplePoints", len(q.samplePoints))
	log.Infof("%d SampleErrors", len(q.sampleErrors))
	log.Infof("%d Run Aggregates", len(q.runAggregates))
	log.Infof("%d Metric Aggregates", len(q.metricAggTypes))

	out, err := save.Save(&q.Input, q.samplePoints, q.sampleErrors, q.runAggregates, q.metricAggValueKeys, q.metricAggTypes, q.metricAggValues)

	// Reset state for next call
	q.samplePoints = nil
	q.sampleErrors = nil
	q.runAggregates = nil
	q.metricAggValueKeys = nil
	q.metricAggTypes = nil
	q.metricAggValues = nil

	// Turn anything not a successful status into an error
	if err == nil && out.GetStatus() != qpb.QuickstoreOutput_SUCCESS {
		err = errors.New(out.GetSummaryOutput())
	}

	return out, err
}

// For dependency injection during unit tests.
type saver interface {
	Save(*qpb.QuickstoreInput,
		[]*pgpb.SamplePoint,
		[]*pgpb.SampleError,
		[]*pgpb.KeyedValue,
		// Metric aggregate value keys
		[]string,
		// Metric aggregate types
		[]string,
		// Metric aggregate values
		[]float64) (qpb.QuickstoreOutput, error)
}

type grpcSaver struct {
	client qspb.QuickstoreClient
}

func (s *grpcSaver) Save(input *qpb.QuickstoreInput,
	samplePoints []*pgpb.SamplePoint,
	sampleErrors []*pgpb.SampleError,
	runAggregates []*pgpb.KeyedValue,
	metricAggValueKeys []string,
	metricAggTypes []string,
	metricAggValues []float64) (qpb.QuickstoreOutput, error) {

	response, err := s.client.Store(context.Background(),
		&qspb.StoreInput{
			QuickstoreInput:      input,
			SamplePoints:         samplePoints,
			SampleErrors:         sampleErrors,
			RunAggregates:        runAggregates,
			AggregateValueKeys:   metricAggValueKeys,
			AggregateValueTypes:  metricAggTypes,
			AggregateValueValues: metricAggValues,
		})

	if err != nil {
		return qpb.QuickstoreOutput{}, err
	}

	if response.GetQuickstoreOutput() == nil {
		return qpb.QuickstoreOutput{}, nil
	}

	return *response.GetQuickstoreOutput(), nil
}
