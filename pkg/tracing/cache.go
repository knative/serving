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
	"sync"

	"github.com/knative/serving/pkg/tracing/config"
	zipkin "github.com/openzipkin/zipkin-go"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	"go.uber.org/zap"
)

// TracerRef performs reference counting for a tracer's collector. This is needed to
// allow for closing a collector only after all it's traces are completed.
type TracerRef struct {
	Tracer      *zipkin.Tracer
	reporterRef *refcountedReporter
}

// Ref adds a reference to the TracerRef. This needs to be unref'd with Done
func (tr *TracerRef) Ref() {
	if tr.reporterRef != nil {
		tr.reporterRef.refs.RLock()
	}
}

// Done unreferences a TracerRef
func (tr *TracerRef) Done() {
	if tr.reporterRef != nil {
		tr.reporterRef.refs.RUnlock()
	}
}

type refcountedReporter struct {
	refs     sync.RWMutex
	reporter zipkinreporter.Reporter
}

// TracerCache manages collector lifecycle and caches the most recently used based
// on Config. It also allows for the immediate creation of new collectors while
// outstanding traces exist for the current collector by reference counting.
//
// Make sure to call Close() when exiting in order to flush outstanding traces.
type TracerCache struct {
	mutex          sync.Mutex
	reporterRef    *refcountedReporter
	cfg            *config.Config
	CreateReporter ReporterFactory
}

func noopTracer() *TracerRef {
	tracer, _ := zipkin.NewTracer(nil)
	return &TracerRef{
		Tracer:      tracer,
		reporterRef: nil,
	}
}

// GetTracerRefOrNoop always returns a TracerRef. In case of an error it logs and returns a Noop tracer.
func (tc *TracerCache) GetTracerRefOrNoop(logger *zap.SugaredLogger, cfg *config.Config, serviceName, hostPort string) *TracerRef {
	tr, err := tc.NewTracerRef(cfg, serviceName, hostPort)
	if err != nil {
		logger.Errorw("Failed to create tracer", zap.Error(err))
		tr = noopTracer()
	}
	return tr
}

// NewTracerRef returns a TracerRef for the passed Config. Make sure to call
// Done on the returned TracerRef when you are done using it.
func (tc *TracerCache) NewTracerRef(cfg *config.Config, serviceName, hostPort string) (*TracerRef, error) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	if cfg == nil {
		return nil, nil
	}

	if tc.cfg == nil || !tc.cfg.Equals(cfg) {
		if tc.reporterRef != nil {
			// Close our reporter when references go to 0
			go func(rr *refcountedReporter) {
				rr.refs.Lock()
				defer rr.refs.Unlock()
				rr.reporter.Close()
			}(tc.reporterRef)
		}
		tc.reporterRef = nil

		tc.cfg = cfg.DeepCopy()
	}

	if tc.reporterRef == nil {
		reporter, err := tc.CreateReporter(cfg)
		if err != nil {
			return nil, err
		}
		// Create our reporter ref
		tc.reporterRef = &refcountedReporter{
			reporter: reporter,
		}
	}

	tracerRef, err := tc.createTracerRef(serviceName, hostPort)
	if err != nil {
		return nil, err
	}
	tracerRef.Ref()
	return tracerRef, nil
}

// Close closes the ZipkinTracer, flushing outstanding traces.
func (tc *TracerCache) Close() {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	if tc.reporterRef != nil {
		tc.reporterRef.refs.Lock()
		tc.reporterRef.reporter.Close()
	}

	tc.reporterRef = nil
	tc.cfg = nil
}

func (tc *TracerCache) createTracerRef(serviceName, hostPort string) (*TracerRef, error) {
	tracer, err := CreateTracer(tc.cfg, tc.reporterRef.reporter, serviceName, hostPort)
	if err != nil {
		return nil, err
	}
	return &TracerRef{
		Tracer:      tracer,
		reporterRef: tc.reporterRef,
	}, nil
}

// ReporterFactory returns a zipkin Reporter given the passed config.Config
type ReporterFactory func(cfg *config.Config) (zipkinreporter.Reporter, error)
