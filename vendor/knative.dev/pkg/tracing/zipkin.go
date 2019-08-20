/*
Copyright 2019 The Knative Authors

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

package tracing

import (
	"contrib.go.opencensus.io/exporter/zipkin"
	zipkinmodel "github.com/openzipkin/zipkin-go/model"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"go.opencensus.io/trace"

	"knative.dev/pkg/tracing/config"
)

// ZipkinReporterFactory is a factory function which creates a reporter given a config
type ZipkinReporterFactory func(*config.Config) (zipkinreporter.Reporter, error)

// CreateZipkinReporter returns a zipkin reporter. If EndpointURL is not specified it returns
// a noop reporter
func CreateZipkinReporter(cfg *config.Config) (zipkinreporter.Reporter, error) {
	if cfg.ZipkinEndpoint == "" {
		return zipkinreporter.NewNoopReporter(), nil
	}
	return httpreporter.NewReporter(cfg.ZipkinEndpoint), nil
}

func WithZipkinExporter(reporterFact ZipkinReporterFactory, endpoint *zipkinmodel.Endpoint) ConfigOption {
	return func(cfg *config.Config) {
		var (
			reporter zipkinreporter.Reporter
			exporter trace.Exporter
		)

		if cfg != nil && cfg.Enable {
			// Initialize our reporter / exporter
			// do this before cleanup to minimize time where we have duplicate exporters
			reporter, err := reporterFact(cfg)
			if err != nil {
				// TODO(greghaynes) log this error
				return
			}
			exporter := zipkin.NewExporter(reporter, endpoint)
			trace.RegisterExporter(exporter)
		}

		// We know this is set because we are called with acquireGlobal lock held
		oct := globalOct
		if oct.exporter != nil {
			trace.UnregisterExporter(oct.exporter)
		}

		if oct.closer != nil {
			// TODO(greghaynes) log this error
			_ = oct.closer.Close()
		}

		oct.closer = reporter
		oct.exporter = exporter
	}
}
