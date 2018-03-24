package b3

import (
	"net/http"

	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation"
)

// ExtractHTTP will extract a span.Context from the HTTP Request if found in
// B3 header format.
func ExtractHTTP(r *http.Request) propagation.Extractor {
	return func() (*model.SpanContext, error) {
		var (
			traceIDHeader      = r.Header.Get(TraceID)
			spanIDHeader       = r.Header.Get(SpanID)
			parentSpanIDHeader = r.Header.Get(ParentSpanID)
			sampledHeader      = r.Header.Get(Sampled)
			flagsHeader        = r.Header.Get(Flags)
		)

		return ParseHeaders(
			traceIDHeader, spanIDHeader, parentSpanIDHeader, sampledHeader,
			flagsHeader,
		)
	}
}

// InjectHTTP will inject a span.Context into a HTTP Request
func InjectHTTP(r *http.Request) propagation.Injector {
	return func(sc model.SpanContext) error {
		if (model.SpanContext{}) == sc {
			return ErrEmptyContext
		}

		if sc.Debug {
			r.Header.Set(Flags, "1")
		} else if sc.Sampled != nil {
			// Debug is encoded as X-B3-Flags: 1. Since Debug implies Sampled,
			// so don't also send "X-B3-Sampled: 1".
			if *sc.Sampled {
				r.Header.Set(Sampled, "1")
			} else {
				r.Header.Set(Sampled, "0")
			}
		}

		if !sc.TraceID.Empty() && sc.ID > 0 {
			r.Header.Set(TraceID, sc.TraceID.String())
			r.Header.Set(SpanID, sc.ID.String())
			if sc.ParentID != nil {
				r.Header.Set(ParentSpanID, sc.ParentID.String())
			}
		}

		return nil
	}
}
