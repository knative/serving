package b3_test

import (
	"net/http"
	"testing"

	zipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	"github.com/openzipkin/zipkin-go/reporter/recorder"
)

func TestHTTPExtractFlagsOnly(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.Flags, "1")

	sc, err := b3.ExtractHTTP(r)()
	if err != nil {
		t.Fatalf("ExtractHTTP failed: %+v", err)
	}

	if want, have := true, sc.Debug; want != have {
		t.Errorf("sc.Debug want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractSampledOnly(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.Sampled, "0")

	sc, err := b3.ExtractHTTP(r)()
	if err != nil {
		t.Fatalf("ExtractHTTP failed: %+v", err)
	}

	if sc.Sampled == nil {
		t.Fatalf("Sampled want %t, have nil", false)
	}

	if want, have := false, *sc.Sampled; want != have {
		t.Errorf("Sampled want %t, have %t", want, have)
	}

	r = newHTTPRequest(t)

	r.Header.Set(b3.Sampled, "1")

	sc, err = b3.ExtractHTTP(r)()
	if err != nil {
		t.Fatalf("ExtractHTTP failed: %+v", err)
	}

	if sc.Sampled == nil {
		t.Fatalf("Sampled want %t, have nil", true)
	}

	if want, have := true, *sc.Sampled; want != have {
		t.Errorf("Sampled want %t, have %t", want, have)
	}
}

func TestHTTPExtractFlagsAndSampledOnly(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.Flags, "1")
	r.Header.Set(b3.Sampled, "1")

	sc, err := b3.ExtractHTTP(r)()
	if err != nil {
		t.Fatalf("ExtractHTTP failed: %+v", err)
	}

	if want, have := true, sc.Debug; want != have {
		t.Errorf("Debug want %+v, have %+v", want, have)
	}

	// Sampled should not be set when sc.Debug is set.
	if sc.Sampled != nil {
		t.Errorf("Sampled want nil, have %+v", *sc.Sampled)
	}
}

func TestHTTPExtractSampledErrors(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.Sampled, "2")

	sc, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidSampledHeader, err; want != have {
		t.Errorf("SpanContext Error want %+v, have %+v", want, have)
	}

	if sc != nil {
		t.Errorf("SpanContext want nil, have: %+v", sc)
	}
}

func TestHTTPExtractFlagsErrors(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.Flags, "2")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidFlagsHeader, err; want != have {
		t.Errorf("SpanContext Error want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractScope(t *testing.T) {
	recorder := &recorder.ReporterRecorder{}
	defer recorder.Close()

	tracer, err := zipkin.NewTracer(recorder, zipkin.WithTraceID128Bit(true))
	if err != nil {
		t.Fatalf("Tracer failed: %+v", err)
	}

	iterations := 1000
	for i := 0; i < iterations; i++ {
		var (
			parent      = tracer.StartSpan("parent")
			child       = tracer.StartSpan("child", zipkin.Parent(parent.Context()))
			wantContext = child.Context()
		)

		r := newHTTPRequest(t)

		b3.InjectHTTP(r)(wantContext)

		haveContext, err := b3.ExtractHTTP(r)()
		if err != nil {
			t.Errorf("ExtractHTTP failed: %+v", err)
		}

		if haveContext == nil {
			t.Fatal("SpanContext want valid value, have nil")
		}

		if want, have := wantContext.TraceID, haveContext.TraceID; want != have {
			t.Errorf("TraceID want %+v, have %+v", want, have)
		}

		if want, have := wantContext.ID, haveContext.ID; want != have {
			t.Errorf("ID want %+v, have %+v", want, have)
		}
		if want, have := *wantContext.ParentID, *haveContext.ParentID; want != have {
			t.Errorf("ParentID want %+v, have %+v", want, have)
		}

		child.Finish()
		parent.Finish()
	}

	// check if we have all spans (2x the iterations: parent+child span)
	if want, have := 2*iterations, len(recorder.Flush()); want != have {
		t.Errorf("Recorded Span Count want %d, have %d", want, have)
	}
}

func TestHTTPExtractTraceIDError(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.TraceID, "invalid_data")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidTraceIDHeader, err; want != have {
		t.Errorf("ExtractHTTP Error want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractSpanIDError(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.SpanID, "invalid_data")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidSpanIDHeader, err; want != have {
		t.Errorf("ExtractHTTP Error want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractTraceIDOnlyError(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.TraceID, "1")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidScope, err; want != have {
		t.Errorf("ExtractHTTP Error want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractSpanIDOnlyError(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.SpanID, "1")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidScope, err; want != have {
		t.Errorf("ExtractHTTP Error want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractParentIDOnlyError(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.ParentSpanID, "1")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidScopeParent, err; want != have {
		t.Errorf("ExtractHTTP Error want %+v, have %+v", want, have)
	}
}

func TestHTTPExtractInvalidParentIDError(t *testing.T) {
	r := newHTTPRequest(t)

	r.Header.Set(b3.TraceID, "1")
	r.Header.Set(b3.SpanID, "2")
	r.Header.Set(b3.ParentSpanID, "invalid_data")

	_, err := b3.ExtractHTTP(r)()

	if want, have := b3.ErrInvalidParentSpanIDHeader, err; want != have {
		t.Errorf("ExtractHTTP Error want %+v, have %+v", want, have)
	}

}

func TestHTTPInjectEmptyContextError(t *testing.T) {
	err := b3.InjectHTTP(nil)(model.SpanContext{})

	if want, have := b3.ErrEmptyContext, err; want != have {
		t.Errorf("HTTPInject Error want %+v, have %+v", want, have)
	}
}

func TestHTTPInjectDebugOnly(t *testing.T) {
	r := newHTTPRequest(t)

	sc := model.SpanContext{
		Debug: true,
	}

	b3.InjectHTTP(r)(sc)

	if want, have := "1", r.Header.Get(b3.Flags); want != have {
		t.Errorf("Flags want %s, have %s", want, have)
	}
}

func TestHTTPInjectSampledOnly(t *testing.T) {
	r := newHTTPRequest(t)

	sampled := false
	sc := model.SpanContext{
		Sampled: &sampled,
	}

	b3.InjectHTTP(r)(sc)

	if want, have := "0", r.Header.Get(b3.Sampled); want != have {
		t.Errorf("Sampled want %s, have %s", want, have)
	}
}

func TestHTTPInjectUnsampledTrace(t *testing.T) {
	r := newHTTPRequest(t)

	sampled := false
	sc := model.SpanContext{
		TraceID: model.TraceID{Low: 1},
		ID:      model.ID(2),
		Sampled: &sampled,
	}

	b3.InjectHTTP(r)(sc)

	if want, have := "0", r.Header.Get(b3.Sampled); want != have {
		t.Errorf("Sampled want %s, have %s", want, have)
	}
}

func TestHTTPInjectSampledAndDebugTrace(t *testing.T) {
	r := newHTTPRequest(t)

	sampled := true
	sc := model.SpanContext{
		TraceID: model.TraceID{Low: 1},
		ID:      model.ID(2),
		Debug:   true,
		Sampled: &sampled,
	}

	b3.InjectHTTP(r)(sc)

	if want, have := "", r.Header.Get(b3.Sampled); want != have {
		t.Errorf("Sampled want empty, have %s", have)
	}

	if want, have := "1", r.Header.Get(b3.Flags); want != have {
		t.Errorf("Debug want %s, have %s", want, have)
	}
}

func newHTTPRequest(t *testing.T) *http.Request {
	r, err := http.NewRequest("test", "", nil)
	if err != nil {
		t.Fatalf("HTTP Request failed: %+v", err)
	}
	return r
}
