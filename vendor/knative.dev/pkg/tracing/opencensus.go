/*
Copyright 2020 The Knative Authors

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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	oczipkin "contrib.go.opencensus.io/exporter/zipkin"
	"github.com/openzipkin/zipkin-go"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"go.opencensus.io/trace"
	"go.uber.org/zap"

	"knative.dev/pkg/tracing/config"
)

// ConfigOption is the interface for adding additional exporters and configuring opencensus tracing.
type ConfigOption func(*config.Config) error

// OpenCensusTracer is responsible for managing and updating configuration of OpenCensus tracing
type OpenCensusTracer struct {
	curCfg        *config.Config
	configOptions []ConfigOption

	closer   io.Closer
	exporter trace.Exporter
}

// OpenCensus tracing keeps state in globals and therefore we can only run one OpenCensusTracer
var (
	octMutex  sync.Mutex
	globalOct *OpenCensusTracer
)

func NewOpenCensusTracer(configOptions ...ConfigOption) *OpenCensusTracer {
	return &OpenCensusTracer{
		configOptions: configOptions,
	}
}

func (oct *OpenCensusTracer) ApplyConfig(cfg *config.Config) error {
	err := oct.acquireGlobal()
	defer octMutex.Unlock()
	if err != nil {
		return err
	}

	// Short circuit if our config hasn't changed.
	if oct.curCfg != nil && oct.curCfg.Equals(cfg) {
		return nil
	}

	// Apply config options
	for _, configOpt := range oct.configOptions {
		if err = configOpt(cfg); err != nil {
			return err
		}
	}

	// Set config
	trace.ApplyConfig(*createOCTConfig(cfg))

	return nil
}

// Deprecated: Use Shutdown.
func (oct *OpenCensusTracer) Finish() error {
	return oct.Shutdown(context.Background())
}

func (oct *OpenCensusTracer) Shutdown(ctx context.Context) error {
	err := oct.acquireGlobal()
	defer octMutex.Unlock()
	if err != nil {
		return errors.New("finish called on OpenTracer which is not the global OpenCensusTracer")
	}

	for _, configOpt := range oct.configOptions {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err = configOpt(nil); err != nil {
				return err
			}
		}
	}
	globalOct = nil

	return nil
}

func (oct *OpenCensusTracer) acquireGlobal() error {
	octMutex.Lock()

	if globalOct == nil {
		globalOct = oct
	} else if globalOct != oct {
		return errors.New("an OpenCensusTracer already exists and only one can be run at a time")
	}

	return nil
}

func createOCTConfig(cfg *config.Config) *trace.Config {
	octCfg := trace.Config{}

	if cfg.Backend != config.None {
		if cfg.Debug {
			octCfg.DefaultSampler = trace.AlwaysSample()
		} else {
			octCfg.DefaultSampler = trace.ProbabilitySampler(cfg.SampleRate)
		}
	} else {
		octCfg.DefaultSampler = trace.NeverSample()
	}

	return &octCfg
}

// WithExporter returns a ConfigOption for use with NewOpenCensusTracer that configures
// it to export traces based on the configuration read from config-tracing.
func WithExporter(name string, logger *zap.SugaredLogger) ConfigOption {
	return WithExporterFull(name, name, logger)
}

// WithExporterFull supports host argument for WithExporter.
// The host arg is used for a value of tag ip="{IP}" so you can use an actual IP. Otherwise,
// the host name must be able to be resolved.
// e.g)
//
//	"name" is a service name like activator-service.
//	"host" is a endpoint IP like activator-service's endpoint IP.
func WithExporterFull(name, host string, logger *zap.SugaredLogger) ConfigOption {
	return func(cfg *config.Config) error {
		var (
			exporter trace.Exporter
			closer   io.Closer
		)
		if cfg != nil {
			switch cfg.Backend {
			case config.Zipkin:
				// If host isn't specified, then zipkin.NewEndpoint will return an error saying that it
				// can't find the host named ''. So, if not specified, default it to this machine's
				// hostname.
				if host == "" {
					n, err := os.Hostname()
					if err != nil {
						return fmt.Errorf("unable to get hostname: %w", err)
					}
					host = n
				}
				if name == "" {
					name = host
				}
				zipEP, err := zipkin.NewEndpoint(name, host)
				if err != nil {
					logger.Errorw("error building zipkin endpoint", zap.Error(err))
					return err
				}
				reporter := httpreporter.NewReporter(cfg.ZipkinEndpoint)
				exporter = oczipkin.NewExporter(reporter, zipEP)
				closer = reporter

			default:
				// Disables tracing.
			}
		}

		if exporter != nil {
			trace.RegisterExporter(exporter)
		}
		// We know this is set because we are called with acquireGlobal lock held
		if globalOct.exporter != nil {
			trace.UnregisterExporter(globalOct.exporter)
		}
		if globalOct.closer != nil {
			globalOct.closer.Close()
		}

		globalOct.exporter = exporter
		globalOct.closer = closer

		return nil
	}
}
