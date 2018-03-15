package b3_test

import (
	"testing"

	zipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	"github.com/openzipkin/zipkin-go/reporter/recorder"
	"google.golang.org/grpc/metadata"
)

func TestGRPCExtractFlagsOnly(t *testing.T) {
	md := metadata.Pairs(b3.Flags, "1")

	sc, err := b3.ExtractGRPC(&md)()
	if err != nil {
		t.Fatalf("ExtractGRPC Failed: %+v", err)
	}

	if want, have := true, sc.Debug; want != have {
		t.Errorf("sc.Debug want %+v, have: %+v", want, have)
	}
}

func TestGRPCExtractSampledOnly(t *testing.T) {
	md := metadata.Pairs(b3.Sampled, "0")

	sc, err := b3.ExtractGRPC(&md)()
	if err != nil {
		t.Fatalf("ExtractGRPC failed: %+v", err)
	}

	if sc.Sampled == nil {
		t.Fatalf("Sampled want %t, have nil", false)
	}

	if want, have := false, *sc.Sampled; want != have {
		t.Errorf("Sampled want %t, have %t", want, have)
	}

	md = metadata.Pairs(b3.Sampled, "1")

	sc, err = b3.ExtractGRPC(&md)()
	if err != nil {
		t.Fatalf("ExtractGRPC failed: %+v", err)
	}

	if sc.Sampled == nil {
		t.Fatalf("Sampled want %t, have nil", true)
	}

	if want, have := true, *sc.Sampled; want != have {
		t.Errorf("Sampled want %t, have %t", want, have)
	}
}

func TestGRPCExtractFlagsAndSampledOnly(t *testing.T) {
	md := metadata.Pairs(
		b3.Flags, "1",
		b3.Sampled, "1",
	)

	sc, err := b3.ExtractGRPC(&md)()
	if err != nil {
		t.Fatalf("ExtractGRPC failed: %+v", err)
	}

	if want, have := true, sc.Debug; want != have {
		t.Errorf("Debug want %t, have %t", want, have)
	}

	if sc.Sampled != nil {
		t.Fatalf("Sampled want nil, have %+v", *sc.Sampled)
	}
}

func TestGRPCExtractSampledErrors(t *testing.T) {
	md := metadata.Pairs(b3.Sampled, "2")

	sc, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidSampledHeader, err; want != have {
		t.Errorf("SpanContext Error want %+v, have %+v", want, have)
	}

	if sc != nil {
		t.Errorf("SpanContext want nil, have: %+v", sc)
	}
}

func TestGRPCExtractFlagsErrors(t *testing.T) {
	md := metadata.Pairs(b3.Flags, "2")

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidFlagsHeader, err; want != have {
		t.Errorf("SpanContext Error want %+v, have %+v", want, have)
	}
}

func TestGRPCExtractScope(t *testing.T) {
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

		md := metadata.MD{}
		b3.InjectGRPC(&md)(wantContext)

		haveContext, err := b3.ExtractGRPC(&md)()
		if err != nil {
			t.Errorf("ExtractGRPC failed: %+v", err)
		}

		if haveContext == nil {
			t.Fatalf("SpanContext want valid value, have nil")
		}

		if want, have := wantContext.TraceID, haveContext.TraceID; want != have {
			t.Errorf("Traceid want %+v, have %+v", want, have)
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

func TestGRPCExtractTraceIDError(t *testing.T) {
	md := metadata.Pairs(b3.TraceID, "invalid_data")

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidTraceIDHeader, err; want != have {
		t.Errorf("ExtractGRPC Error want %+v, have %+v", want, have)
	}
}

func TestGRPCExtractSpanIDError(t *testing.T) {
	md := metadata.Pairs(b3.SpanID, "invalid_data")

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidSpanIDHeader, err; want != have {
		t.Errorf("ExtractGRPC Error want %+v, have %+v", want, have)
	}
}

func TestGRPCExtractTraceIDOnlyError(t *testing.T) {
	md := metadata.Pairs(b3.TraceID, "1")

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidScope, err; want != have {
		t.Errorf("ExtractGRPC Error want %+v, got %+v", want, have)
	}
}

func TestGRPCExtractSpanIDOnlyError(t *testing.T) {
	md := metadata.Pairs(b3.SpanID, "1")

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidScope, err; want != have {
		t.Errorf("ExtractGRPC Error want %+v, have %+v", want, have)
	}
}

func TestGRPCExtractParentIDOnlyError(t *testing.T) {
	md := metadata.Pairs(b3.ParentSpanID, "1")

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidScopeParent, err; want != have {
		t.Errorf("ExtractGRPC Error want %+v, have %+v", want, have)
	}
}

func TestGRPCExtractInvalidParentIDError(t *testing.T) {
	md := metadata.Pairs(
		b3.TraceID, "1",
		b3.SpanID, "2",
		b3.ParentSpanID, "invalid_data",
	)

	_, err := b3.ExtractGRPC(&md)()

	if want, have := b3.ErrInvalidParentSpanIDHeader, err; want != have {
		t.Errorf("ExtractGRPC Error want %+v, have %+v", want, have)
	}
}

func TestGRPCInjectEmptyContextError(t *testing.T) {
	err := b3.InjectGRPC(nil)(model.SpanContext{})

	if want, have := b3.ErrEmptyContext, err; want != have {
		t.Errorf("GRPCInject Error want %+v, have %+v", want, have)
	}
}

func TestGRPCInjectDebugOnly(t *testing.T) {
	md := &metadata.MD{}

	sc := model.SpanContext{
		Debug: true,
	}

	b3.InjectGRPC(md)(sc)

	if want, have := "1", b3.GetGRPCHeader(md, b3.Flags); want != have {
		t.Errorf("Flags want %s, have %s", want, have)
	}
}

func TestGRPCInjectSampledOnly(t *testing.T) {
	md := &metadata.MD{}

	sampled := false
	sc := model.SpanContext{
		Sampled: &sampled,
	}

	b3.InjectGRPC(md)(sc)

	if want, have := "0", b3.GetGRPCHeader(md, b3.Sampled); want != have {
		t.Errorf("Sampled want %s, have %s", want, have)
	}
}

func TestGRPCInjectUnsampledTrace(t *testing.T) {
	md := &metadata.MD{}

	sampled := false
	sc := model.SpanContext{
		TraceID: model.TraceID{Low: 1},
		ID:      model.ID(2),
		Sampled: &sampled,
	}

	b3.InjectGRPC(md)(sc)

	if want, have := "0", b3.GetGRPCHeader(md, b3.Sampled); want != have {
		t.Errorf("Sampled want %s, have %s", want, have)
	}
}

func TestGRPCInjectSampledAndDebugTrace(t *testing.T) {
	md := &metadata.MD{}

	sampled := true
	sc := model.SpanContext{
		TraceID: model.TraceID{Low: 1},
		ID:      model.ID(2),
		Debug:   true,
		Sampled: &sampled,
	}

	b3.InjectGRPC(md)(sc)

	if want, have := "", b3.GetGRPCHeader(md, b3.Sampled); want != have {
		t.Errorf("Sampled want empty, have %s", have)
	}

	if want, have := "1", b3.GetGRPCHeader(md, b3.Flags); want != have {
		t.Errorf("Debug want %s, have %s", want, have)
	}
}
