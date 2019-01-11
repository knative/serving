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
	"errors"
	"testing"
	"time"

	"github.com/knative/serving/pkg/tracing/config"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	reporterrecorder "github.com/openzipkin/zipkin-go/reporter/recorder"
)

func TestGetTracerDefaultConfig(t *testing.T) {
	reporter := reporterrecorder.NewReporter()
	tc := TracerCache{
		CreateReporter: reporterFactoryFactory([]zipkinreporter.Reporter{reporter}),
	}
	defer tc.Close()
	cfg := config.Config{
		Enable: true,
		Debug:  true,
	}

	tr, err := tc.NewTracerRef(&cfg, "test-service1", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to get tracer for test-service1: %v", err)
	}
	defer tr.Done()

	span1 := tr.Tracer.StartSpan("service1-span")
	span1.Finish()

	gotSpans := reporter.Flush()
	if len(gotSpans) != 1 {
		t.Errorf("Expected %d spans in reporter, got %d", 1, len(gotSpans))
	}
}

func TestGetTracerSharesReporter(t *testing.T) {
	reporter := reporterrecorder.NewReporter()
	tc := TracerCache{
		CreateReporter: reporterFactoryFactory([]zipkinreporter.Reporter{reporter}),
	}
	defer tc.Close()
	cfg := config.Config{
		Enable:      true,
		Debug:       true,
		EndpointURL: "test-endpoint",
	}

	tr, err := tc.NewTracerRef(&cfg, "test-service1", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to get tracer for test-service1: %v", err)
	}
	defer tr.Done()

	tr2, err := tc.NewTracerRef(&cfg, "test-service2", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to get tracer for test-service2: %v", err)
	}
	defer tr2.Done()

	span1 := tr.Tracer.StartSpan("service1-span")
	span2 := tr2.Tracer.StartSpan("service2-span")

	span1.Finish()
	span2.Finish()

	gotSpans := reporter.Flush()
	if len(gotSpans) != 2 {
		t.Errorf("Expected %d spans in reporter, got %d", 2, len(gotSpans))
	}
}

func TestGetTracerNewRecorder(t *testing.T) {
	reporter1 := reporterrecorder.NewReporter()
	reporter2 := reporterrecorder.NewReporter()
	tc := TracerCache{
		CreateReporter: reporterFactoryFactory([]zipkinreporter.Reporter{reporter1, reporter2}),
	}
	defer tc.Close()
	cfg := config.Config{
		Enable:      true,
		Debug:       true,
		EndpointURL: "test-endpoint",
	}

	tr, err := tc.NewTracerRef(&cfg, "test-service1", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to get tracer for test-service1: %v", err)
	}
	defer tr.Done()

	cfg.EndpointURL = "foo"
	tr2, err := tc.NewTracerRef(&cfg, "test-service2", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to get tracer for test-service2: %v", err)
	}
	defer tr2.Done()

	span1 := tr.Tracer.StartSpan("service1-span")
	span2 := tr2.Tracer.StartSpan("service2-span")

	span1.Finish()
	span2.Finish()

	rep1Spans := reporter1.Flush()
	rep2Spans := reporter2.Flush()

	if len(rep1Spans) != 1 {
		t.Errorf("Expected %d spans in reporter1, got %d", 1, len(rep1Spans))
	}

	if len(rep2Spans) != 1 {
		t.Errorf("Expected %d spans in reporter2, got %d", 1, len(rep2Spans))
	}
}

func TestTraceDuringCacheClose(t *testing.T) {
	reporter := reporterrecorder.NewReporter()
	tc := TracerCache{
		CreateReporter: reporterFactoryFactory([]zipkinreporter.Reporter{reporter}),
	}
	cfg := config.Config{
		Enable: true,
		Debug:  true,
	}

	tr, err := tc.NewTracerRef(&cfg, "test-service1", "localhost:1234")
	if err != nil {
		t.Errorf("Failed to get tracer for test-service1: %v", err)
	}

	// Close the cache and defer so the goroutine runs
	go tc.Close()
	time.Sleep(time.Millisecond * 100)

	span1 := tr.Tracer.StartSpan("service1-span")
	span1.Finish()

	gotSpans := reporter.Flush()
	if len(gotSpans) != 1 {
		t.Errorf("Expected %d spans in reporter, got %d", 1, len(gotSpans))
	}

	tr.Done()
}

func reporterFactoryFactory(reporters []zipkinreporter.Reporter) ReporterFactory {
	i := 0
	return func(cfg *config.Config) (zipkinreporter.Reporter, error) {
		if len(reporters) <= i {
			return nil, errors.New("No reporter available")
		}
		ret := reporters[i]
		i++
		return ret, nil
	}
}
