/*
Package recorder implements a reporter to record spans in v2 format.
*/
package recorder

import (
	"sync"

	"github.com/openzipkin/zipkin-go/model"
)

// ReporterRecorder records Zipkin spans.
type ReporterRecorder struct {
	mtx   sync.Mutex
	spans []model.SpanModel
}

// NewReporter returns a new recording reporter.
func NewReporter() *ReporterRecorder {
	return &ReporterRecorder{}
}

// Send adds the provided span to the span list held by the recorder.
func (r *ReporterRecorder) Send(span model.SpanModel) {
	r.mtx.Lock()
	r.spans = append(r.spans, span)
	r.mtx.Unlock()
}

// Flush returns all recorded spans and clears its internal span storage
func (r *ReporterRecorder) Flush() []model.SpanModel {
	r.mtx.Lock()
	spans := r.spans
	r.spans = nil
	r.mtx.Unlock()
	return spans
}

// Close flushes the reporter
func (r *ReporterRecorder) Close() error {
	r.Flush()
	return nil
}
