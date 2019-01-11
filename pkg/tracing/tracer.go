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
	"math/rand"

	zipkin "github.com/openzipkin/zipkin-go"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"

	"github.com/knative/serving/pkg/tracing/config"
)

// CreateReporter returns a zipkin reporter. If EndpointURL is not specified it returns
// a noop reporter
func CreateReporter(cfg *config.Config) (zipkinreporter.Reporter, error) {
	if cfg.EndpointURL == "" {
		return zipkinreporter.NewNoopReporter(), nil
	}
	return httpreporter.NewReporter(cfg.EndpointURL), nil
}

// CreateSampler returns a sampler matching the passed config
func CreateSampler(cfg *config.Config) (zipkin.Sampler, error) {
	if !cfg.Enable {
		return zipkin.NeverSample, nil
	} else if cfg.Debug {
		return zipkin.AlwaysSample, nil
	} else {
		sampler, err := zipkin.NewBoundarySampler(cfg.SampleRate, rand.Int63())
		if err != nil {
			return nil, err
		}
		return sampler, nil
	}
}

// CreateTracer returns a tracer for the passed config which uses the passed reporter
func CreateTracer(cfg *config.Config, reporter zipkinreporter.Reporter, serviceName, hostPort string) (*zipkin.Tracer, error) {
	endpoint, err := zipkin.NewEndpoint(serviceName, hostPort)
	if err != nil {
		return nil, err
	}

	sampler, err := CreateSampler(cfg)
	if err != nil {
		return nil, err
	}
	return zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(endpoint), zipkin.WithSampler(sampler))
}
