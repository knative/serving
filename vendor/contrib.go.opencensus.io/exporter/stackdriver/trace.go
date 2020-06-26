// Copyright 2017, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stackdriver

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tracingclient "cloud.google.com/go/trace/apiv2"
	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"
	"google.golang.org/api/support/bundler"
	tracepb "google.golang.org/genproto/googleapis/devtools/cloudtrace/v2"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
)

// traceExporter is an implementation of trace.Exporter that uploads spans to
// Stackdriver.
//
type traceExporter struct {
	o         Options
	projectID string
	bundler   *bundler.Bundler
	// uploadFn defaults to uploadSpans; it can be replaced for tests.
	uploadFn func(spans []*tracepb.Span)
	overflowLogger
	client *tracingclient.Client
}

var _ trace.Exporter = (*traceExporter)(nil)

func newTraceExporter(o Options) (*traceExporter, error) {
	ctx := o.Context
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := tracingclient.NewClient(ctx, o.TraceClientOptions...)
	if err != nil {
		return nil, fmt.Errorf("stackdriver: couldn't initialize trace client: %v", err)
	}
	return newTraceExporterWithClient(o, client), nil
}

const defaultBufferedByteLimit = 8 * 1024 * 1024

func newTraceExporterWithClient(o Options, c *tracingclient.Client) *traceExporter {
	e := &traceExporter{
		projectID: o.ProjectID,
		client:    c,
		o:         o,
	}
	b := bundler.NewBundler((*tracepb.Span)(nil), func(bundle interface{}) {
		e.uploadFn(bundle.([]*tracepb.Span))
	})
	if o.BundleDelayThreshold > 0 {
		b.DelayThreshold = o.BundleDelayThreshold
	} else {
		b.DelayThreshold = 2 * time.Second
	}
	if o.BundleCountThreshold > 0 {
		b.BundleCountThreshold = o.BundleCountThreshold
	} else {
		b.BundleCountThreshold = 50
	}
	if o.NumberOfWorkers > 0 {
		b.HandlerLimit = o.NumberOfWorkers
	}
	// The measured "bytes" are not really bytes, see exportReceiver.
	b.BundleByteThreshold = b.BundleCountThreshold * 200
	b.BundleByteLimit = b.BundleCountThreshold * 1000
	if o.TraceSpansBufferMaxBytes > 0 {
		b.BufferedByteLimit = o.TraceSpansBufferMaxBytes
	} else {
		b.BufferedByteLimit = defaultBufferedByteLimit
	}

	e.bundler = b
	e.uploadFn = e.uploadSpans
	return e
}

// ExportSpan exports a SpanData to Stackdriver Trace.
func (e *traceExporter) ExportSpan(s *trace.SpanData) {
	protoSpan := protoFromSpanData(s, e.projectID, e.o.Resource)
	protoSize := proto.Size(protoSpan)
	err := e.bundler.Add(protoSpan, protoSize)
	switch err {
	case nil:
		return
	case bundler.ErrOversizedItem:
	case bundler.ErrOverflow:
		e.overflowLogger.log()
	default:
		e.o.handleError(err)
	}
}

// Flush waits for exported trace spans to be uploaded.
//
// This is useful if your program is ending and you do not want to lose recent
// spans.
func (e *traceExporter) Flush() {
	e.bundler.Flush()
}

func (e *traceExporter) pushTraceSpans(ctx context.Context, node *commonpb.Node, r *resourcepb.Resource, spans []*trace.SpanData) (int, error) {
	ctx, span := trace.StartSpan(
		ctx,
		"contrib.go.opencensus.io/exporter/stackdriver.PushTraceSpans",
		trace.WithSampler(trace.NeverSample()),
	)
	defer span.End()
	span.AddAttributes(trace.Int64Attribute("num_spans", int64(len(spans))))

	protoSpans := make([]*tracepb.Span, 0, len(spans))

	res := e.o.Resource
	if r != nil {
		res = e.o.MapResource(resourcepbToResource(r))
	}

	for _, span := range spans {
		protoSpans = append(protoSpans, protoFromSpanData(span, e.projectID, res))
	}

	req := tracepb.BatchWriteSpansRequest{
		Name:  "projects/" + e.projectID,
		Spans: protoSpans,
	}
	// Create a never-sampled span to prevent traces associated with exporter.
	ctx, cancel := newContextWithTimeout(ctx, e.o.Timeout)
	defer cancel()

	err := e.client.BatchWriteSpans(ctx, &req)

	if err != nil {
		return len(spans), err
	}
	return 0, nil
}

// uploadSpans uploads a set of spans to Stackdriver.
func (e *traceExporter) uploadSpans(spans []*tracepb.Span) {
	req := tracepb.BatchWriteSpansRequest{
		Name:  "projects/" + e.projectID,
		Spans: spans,
	}
	// Create a never-sampled span to prevent traces associated with exporter.
	ctx, cancel := newContextWithTimeout(e.o.Context, e.o.Timeout)
	defer cancel()
	ctx, span := trace.StartSpan(
		ctx,
		"contrib.go.opencensus.io/exporter/stackdriver.uploadSpans",
		trace.WithSampler(trace.NeverSample()),
	)
	defer span.End()
	span.AddAttributes(trace.Int64Attribute("num_spans", int64(len(spans))))

	err := e.client.BatchWriteSpans(ctx, &req)
	if err != nil {
		span.SetStatus(trace.Status{Code: 2, Message: err.Error()})
		e.o.handleError(err)
	}
}

// overflowLogger ensures that at most one overflow error log message is
// written every 5 seconds.
type overflowLogger struct {
	mu    sync.Mutex
	pause bool
	accum int
}

func (o *overflowLogger) delay() {
	o.pause = true
	time.AfterFunc(5*time.Second, func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		switch {
		case o.accum == 0:
			o.pause = false
		case o.accum == 1:
			log.Println("OpenCensus Stackdriver exporter: failed to upload span: buffer full")
			o.accum = 0
			o.delay()
		default:
			log.Printf("OpenCensus Stackdriver exporter: failed to upload %d spans: buffer full", o.accum)
			o.accum = 0
			o.delay()
		}
	})
}

func (o *overflowLogger) log() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.pause {
		log.Println("OpenCensus Stackdriver exporter: failed to upload span: buffer full")
		o.delay()
	} else {
		o.accum++
	}
}
