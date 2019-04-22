package tracing

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/tracing/config"
	openzipkin "github.com/openzipkin/zipkin-go"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	reporterrecorder "github.com/openzipkin/zipkin-go/reporter/recorder"
	"go.opencensus.io/trace"
)

func TestOpenCensusTracerGlobalLifecycle(t *testing.T) {
	reporter := reporterrecorder.NewReporter()
	defer reporter.Close()
	oct := newOCT(reporter)
	// Apply a config to make us the global OCT
	if err := oct.ApplyConfig(&config.Config{}); err != nil {
		t.Fatalf("Failed to ApplyConfig on tracer: %v", err)
	}

	otherOCT := newOCT(reporter)
	if err := otherOCT.ApplyConfig(&config.Config{}); err == nil {
		t.Fatalf("Expected error when applying config to second OCT.")
	}

	if err := oct.Finish(); err != nil {
		t.Fatalf("Failed to finish OCT: %v", err)
	}

	if err := otherOCT.ApplyConfig(&config.Config{}); err != nil {
		t.Fatalf("Failed to ApplyConfig on OtherOCT after finishing OCT: %v", err)
	}
	otherOCT.Finish()
}

func TestOpenCensusTracerApplyConfig(t *testing.T) {
	tcs := []struct {
		name          string
		cfg           config.Config
		expect        *config.Config
		reporterError bool
	}{{
		name: "Disabled config",
		cfg: config.Config{
			Enable: false,
		},
		expect: nil,
	}, {
		name: "Endpoint specified",
		cfg: config.Config{
			Enable:         true,
			ZipkinEndpoint: "test-endpoint:1234",
		},
		expect: &config.Config{
			Enable:         true,
			ZipkinEndpoint: "test-endpoint:1234",
		},
	}}

	endpoint, _ := openzipkin.NewEndpoint("test", "localhost:1234")
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var gotCfg *config.Config
			reporter := reporterrecorder.NewReporter()
			oct := NewOpenCensusTracer(WithZipkinExporter(func(cfg *config.Config) (zipkinreporter.Reporter, error) {
				gotCfg = cfg
				if tc.reporterError {
					return nil, errors.New("Induced reporter factory error")
				}
				return reporter, nil
			}, endpoint))

			if err := oct.ApplyConfig(&tc.cfg); (err == nil) == tc.reporterError {
				t.Errorf("Failed to apply config: %v", err)
			}
			if diff := cmp.Diff(gotCfg, tc.expect); diff != "" {
				t.Errorf("Got tracer config (-want, +got) = %v", diff)
			}

			oct.Finish()
		})
	}
}

func TestCreateOCTConfig(t *testing.T) {
	tcs := []struct {
		name   string
		cfg    config.Config
		expect trace.Config
	}{{
		name: "Default",
		cfg:  config.Config{},
		expect: trace.Config{
			DefaultSampler: trace.NeverSample(),
		},
	}, {
		name: "Disabled",
		cfg: config.Config{
			Enable: false,
		},
		expect: trace.Config{
			DefaultSampler: trace.NeverSample(),
		},
	}, {
		name: "Debug",
		cfg: config.Config{
			Enable: true,
			Debug:  true,
		},
		expect: trace.Config{
			DefaultSampler: trace.AlwaysSample(),
		},
	}, {
		name: "Debug disabled",
		cfg: config.Config{
			Enable: false,
			Debug:  true,
		},
		expect: trace.Config{
			DefaultSampler: trace.NeverSample(),
		},
	}, {
		name: "percent sampler",
		cfg: config.Config{
			Enable:     true,
			Debug:      false,
			SampleRate: 0.5,
		},
		expect: trace.Config{
			DefaultSampler: trace.ProbabilitySampler(0.5),
		},
	}}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			octCfg := createOCTConfig(&tc.cfg)

			// Create 100 traceIDs and make sure our expected sampler samples the same as what we get
			for i := 0; i < 100; i++ {
				spanID := make([]byte, 8)
				rand.Read(spanID)
				param := trace.SamplingParameters{}
				copy(param.SpanID[:], spanID)
				if tc.expect.DefaultSampler(param).Sample != octCfg.DefaultSampler(param).Sample {
					t.Errorf("Sampler for config did not match expected sample value for trace.")
				}
			}
		})
	}
}

func newOCT(reporter zipkinreporter.Reporter) *OpenCensusTracer {
	endpoint, _ := openzipkin.NewEndpoint("test", "localhost:1234")
	return NewOpenCensusTracer(WithZipkinExporter(func(cfg *config.Config) (zipkinreporter.Reporter, error) {
		return reporter, nil
	}, endpoint))
}
