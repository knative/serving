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

// loadgenerator.go provides a wrapper on fortio load generator.

package loadgenerator

import (
	"fmt"
	"time"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/periodic"
)

const (
	p50 = 50.0
	p90 = 90.0
	p99 = 99.0
)

// GeneratorOptions provides knobs to run the perf test
type GeneratorOptions struct {
	Duration       time.Duration
	NumThreads     int
	NumConnections int
	URL            string
	Domain         string
	RequestTimeout time.Duration
	QPS            float64
}

// GeneratorResults contains the results of running the per test
type GeneratorResults struct {
	Result *fhttp.HTTPRunnerResults
}

// CreateRunnerOptions sets up the fortio client with the knobs needed to run the load test
func (g *GeneratorOptions) CreateRunnerOptions(resolvableDomain bool) *fhttp.HTTPRunnerOptions {
	o := fhttp.NewHTTPOptions(g.URL)

	o.NumConnections = g.NumConnections
	o.HTTPReqTimeOut = g.RequestTimeout

	// If the url does not contains a resolvable domain, we need to add the domain as a header
	if !resolvableDomain {
		o.AddAndValidateExtraHeader(fmt.Sprintf("Host: %s", g.Domain))
	}

	return &fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			Duration:    g.Duration,
			NumThreads:  g.NumThreads,
			Percentiles: []float64{p50, p90, p99},
			QPS:         g.QPS,
		},
		HTTPOptions:        *o,
		AllowInitialErrors: true,
	}
}

// RunLoadTest runs the load test with fortio and returns the response
func (g *GeneratorOptions) RunLoadTest(resolvableDomain bool) (*GeneratorResults, error) {
	r, err := fhttp.RunHTTPTest(g.CreateRunnerOptions(resolvableDomain))
	return &GeneratorResults{Result: r}, err
}
