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
	"reflect"
	"testing"

	zipkin "github.com/openzipkin/zipkin-go"
	reporterrecorder "github.com/openzipkin/zipkin-go/reporter/recorder"

	"github.com/knative/serving/pkg/tracing/config"
)

func TestCreateReporter(t *testing.T) {
	rep, err := CreateReporter(&config.Config{
		EndpointURL: "http://localhost:8000",
	})
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}

	err = rep.Close()
	if err != nil {
		t.Errorf("Failed to close reporter: %v", err)
	}

	_, err = CreateReporter(&config.Config{})
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}
}

func TestCreateSampler(t *testing.T) {
	tt := []struct {
		name   string
		cfg    config.Config
		expect zipkin.Sampler
	}{
		{
			name:   "Default",
			cfg:    config.Config{},
			expect: zipkin.NeverSample,
		},
		{
			name: "Disabled",
			cfg: config.Config{
				Enable: false,
			},
			expect: zipkin.NeverSample,
		},
		{
			name: "Debug",
			cfg: config.Config{
				Enable: true,
				Debug:  true,
			},
			expect: zipkin.AlwaysSample,
		},
		{
			name: "Debug disabled",
			cfg: config.Config{
				Enable: false,
				Debug:  true,
			},
			expect: zipkin.NeverSample,
		},
		{
			name: "Boundary Sampler Disabled",
			cfg: config.Config{
				Enable:     true,
				Debug:      false,
				SampleRate: 0,
			},
			expect: zipkin.NeverSample,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			samp, err := CreateSampler(&tc.cfg)
			if err != nil {
				t.Errorf("Failed to create sampler: %v", err)
			} else {
				sampRef := reflect.ValueOf(samp)
				expectRef := reflect.ValueOf(tc.expect)
				if sampRef.Pointer() != expectRef.Pointer() {
					t.Errorf("Expected sampler %v, got %v", tc.expect, samp)
				}
			}
		})
	}

	samp, err := CreateSampler(&config.Config{
		Enable:     true,
		Debug:      false,
		SampleRate: 0.5,
	})
	if err != nil {
		t.Errorf("Failed to create sampler: %v", err)
	}
	sampRef := reflect.ValueOf(samp)
	if sampRef.Pointer() == reflect.ValueOf(zipkin.NeverSample).Pointer() {
		t.Error("Expecting boundary sampler, got NeverSample")
	} else if sampRef.Pointer() == reflect.ValueOf(zipkin.AlwaysSample).Pointer() {
		t.Error("Expecting boundary sampler, got AlwaysSample")
	}
}

func TestCreateTracer(t *testing.T) {
	cfg := config.Config{
		Enable: true,
		Debug:  true,
	}

	reporter := reporterrecorder.NewReporter()
	defer reporter.Close()

	tracer, err := CreateTracer(&cfg, reporter, "testservice", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}

	testSpan := tracer.StartSpan("test-span")
	testSpan.Finish()

	spans := reporter.Flush()
	if len(spans) != 1 {
		t.Errorf("Expected to get 1 span, got %d", len(spans))
	}

	gotSpan := spans[0]
	if gotSpan.Name != "test-span" {
		t.Errorf("Expected span name test-span, got %q", gotSpan.Name)
	}
}
