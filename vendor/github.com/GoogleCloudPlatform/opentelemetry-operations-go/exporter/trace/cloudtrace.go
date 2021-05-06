// Copyright 2019 OpenTelemetry Authors
// Copyright 2021 Google LLC
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

package trace

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	traceapi "cloud.google.com/go/trace/apiv2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// Option is function type that is passed to the exporter initialization function.
type Option func(*options)

// DisplayNameFormatter is is a function that produces the display name of a span
// given its SpanSnapshot
type DisplayNameFormatter func(*sdktrace.SpanSnapshot) string

// options contains options for configuring the exporter.
type options struct {
	// ProjectID is the identifier of the Stackdriver
	// project the user is uploading the stats data to.
	// If not set, this will default to your "Application Default Credentials".
	// For details see: https://developers.google.com/accounts/docs/application-default-credentials.
	//
	// It will be used in the project_id label of a Stackdriver monitored
	// resource if the resource does not inherently belong to a specific
	// project, e.g. on-premise resource like k8s_container or generic_task.
	ProjectID string

	// Location is the identifier of the GCP or AWS cloud region/zone in which
	// the data for a resource is stored.
	// If not set, it will default to the location provided by the metadata server.
	//
	// It will be used in the location label of a Stackdriver monitored resource
	// if the resource does not inherently belong to a specific project, e.g.
	// on-premise resource like k8s_container or generic_task.
	Location string

	// OnError is the hook to be called when there is
	// an error uploading the stats or tracing data.
	// If no custom hook is set, errors are logged.
	// Optional.
	OnError func(err error)

	// TraceClientOptions are additional options to be passed
	// to the underlying Stackdriver Trace API client.
	// Optional.
	TraceClientOptions []option.ClientOption

	// BatchSpanProcessorOptions are additional options to be based
	// to the underlying BatchSpanProcessor when call making a new export pipeline.
	BatchSpanProcessorOptions []sdktrace.BatchSpanProcessorOption

	// DefaultTraceAttributes will be appended to every span that is exported to
	// Stackdriver Trace.
	DefaultTraceAttributes []attribute.KeyValue

	// Context allows you to provide a custom context for API calls.
	//
	// This context will be used several times: first, to create Stackdriver
	// trace and metric clients, and then every time a new batch of traces or
	// stats needs to be uploaded.
	//
	// Do not set a timeout on this context. Instead, set the Timeout option.
	//
	// If unset, context.Background() will be used.
	Context context.Context

	// Timeout for all API calls. If not set, defaults to 5 seconds.
	Timeout time.Duration

	// DisplayNameFormatter is a function that produces the display name of a span
	// given its SpanSnapshot.
	// Optional. Default display name for SpanSnapshot s is "{s.Name}"
	DisplayNameFormatter
}

// WithProjectID sets Google Cloud Platform project as projectID.
// Without using this option, it automatically detects the project ID
// from the default credential detection process.
// Please find the detailed order of the default credentail detection proecess on the doc:
// https://godoc.org/golang.org/x/oauth2/google#FindDefaultCredentials
func WithProjectID(projectID string) func(o *options) {
	return func(o *options) {
		o.ProjectID = projectID
	}
}

// WithOnError sets the hook to be called when there is an error
// occurred on uploading the span data to Stackdriver.
// If no custom hook is set, errors are logged.
func WithOnError(onError func(err error)) func(o *options) {
	return func(o *options) {
		o.OnError = onError
	}
}

// WithTraceClientOptions sets additionial client options for tracing.
func WithTraceClientOptions(opts []option.ClientOption) func(o *options) {
	return func(o *options) {
		o.TraceClientOptions = opts
	}
}

// WithContext sets the context that trace exporter and metric exporter
// relies on.
func WithContext(ctx context.Context) func(o *options) {
	return func(o *options) {
		o.Context = ctx
	}
}

// WithTimeout sets the timeout for trace exporter and metric exporter
func WithTimeout(t time.Duration) func(o *options) {
	return func(o *options) {
		o.Timeout = t
	}
}

// WithDisplayNameFormatter sets the way span's display names will be
// generated from SpanSnapshot
func WithDisplayNameFormatter(f DisplayNameFormatter) func(o *options) {
	return func(o *options) {
		o.DisplayNameFormatter = f
	}
}

// WithDefaultTraceAttributes sets the attributes that will be appended
// to every span by default
func WithDefaultTraceAttributes(attr map[string]interface{}) func(o *options) {
	return func(o *options) {
		o.DefaultTraceAttributes = createKeyValueAttributes(attr)
	}
}

func (o *options) handleError(err error) {
	if o.OnError != nil {
		o.OnError(err)
		return
	}
	log.Printf("Failed to export to Stackdriver: %v", err)
}

// defaultTimeout is used as default when timeout is not set in newContextWithTimout.
const defaultTimeout = 5 * time.Second

// Exporter is a trace exporter that uploads data to Stackdriver.
//
// TODO(yoshifumi): add a metrics exporter once the spec definition
// process and the sampler implementation are done.
type Exporter struct {
	traceExporter *traceExporter
}

// InstallNewPipeline instantiates a NewExportPipeline and registers it globally.
func InstallNewPipeline(opts []Option, topts ...sdktrace.TracerProviderOption) (trace.TracerProvider, func(), error) {
	tp, shutdown, err := NewExportPipeline(opts, topts...)
	if err != nil {
		return nil, nil, err
	}
	otel.SetTracerProvider(tp)
	return tp, shutdown, err
}

// NewExportPipeline sets up a complete export pipeline with the recommended setup
// for trace provider. Returns provider, shutdown function, and errors.
func NewExportPipeline(opts []Option, topts ...sdktrace.TracerProviderOption) (trace.TracerProvider, func(), error) {
	// TODO(suereth): Don't flesh options twice.
	o := options{Context: context.Background()}
	for _, opt := range opts {
		opt(&o)
	}
	exporter, err := newExporterWithOptions(&o)
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		append(topts,
			sdktrace.WithBatcher(exporter, o.BatchSpanProcessorOptions...))...)
	return tp, func() {
		tp.Shutdown(context.Background())
	}, nil
}

// NewExporter creates a new Exporter thats implements trace.Exporter.
func NewExporter(opts ...Option) (*Exporter, error) {
	o := options{Context: context.Background()}
	for _, opt := range opts {
		opt(&o)
	}
	return newExporterWithOptions(&o)
}

func newExporterWithOptions(o *options) (*Exporter, error) {
	if o.ProjectID == "" {
		creds, err := google.FindDefaultCredentials(o.Context, traceapi.DefaultAuthScopes()...)
		if err != nil {
			return nil, fmt.Errorf("stackdriver: %v", err)
		}
		if creds.ProjectID == "" {
			return nil, errors.New("stackdriver: no project found with application default credentials")
		}
		o.ProjectID = creds.ProjectID
	}
	te, err := newTraceExporter(o)
	if err != nil {
		return nil, err
	}

	return &Exporter{
		traceExporter: te,
	}, nil
}

func newContextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, func()) {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

// ExportSpans exports a SpanSnapshot to Stackdriver Trace.
func (e *Exporter) ExportSpans(ctx context.Context, spanData []*sdktrace.SpanSnapshot) error {
	if len(e.traceExporter.o.DefaultTraceAttributes) > 0 {
		for _, sd := range spanData {
			sd.Attributes = append(sd.Attributes, e.traceExporter.o.DefaultTraceAttributes...)
		}
	}

	return e.traceExporter.ExportSpans(ctx, spanData)
}

// Shutdown waits for exported data to be uploaded.
//
// For our purposes it closed down the client.
func (e *Exporter) Shutdown(ctx context.Context) error {
	return e.traceExporter.Shutdown(ctx)
}

func createKeyValueAttributes(attr map[string]interface{}) []attribute.KeyValue {
	kv := make([]attribute.KeyValue, 0, len(attr))

	for k, v := range attr {
		switch val := v.(type) {
		case bool:
			kv = append(kv, attribute.Bool(k, val))
		case int64:
			kv = append(kv, attribute.Int64(k, val))
		case float64:
			kv = append(kv, attribute.Float64(k, val))
		case string:
			kv = append(kv, attribute.String(k, val))
		}
	}

	return kv
}
