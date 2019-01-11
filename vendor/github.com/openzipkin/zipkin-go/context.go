package zipkin

import (
	"context"
)

var defaultNoopSpan = &noopSpan{}

// SpanFromContext retrieves a Zipkin Span from Go's context propagation
// mechanism if found. If not found, returns nil.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey).(Span); ok {
		return s
	}
	return nil
}

// SpanOrNoopFromContext retrieves a Zipkin Span from Go's context propagation
// mechanism if found. If not found, returns a noopSpan.
// This function typically is used for modules that want to provide existing
// Zipkin spans with additional data, but can't guarantee that spans are
// properly propagated. It is preferred to use SpanFromContext() and test for
// Nil instead of using this function.
func SpanOrNoopFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey).(Span); ok {
		return s
	}
	return defaultNoopSpan
}

// NewContext stores a Zipkin Span into Go's context propagation mechanism.
func NewContext(ctx context.Context, s Span) context.Context {
	return context.WithValue(ctx, spanKey, s)
}

type ctxKey struct{}

var spanKey = ctxKey{}
