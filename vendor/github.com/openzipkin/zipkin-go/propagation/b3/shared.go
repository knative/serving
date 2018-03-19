package b3

import "errors"

// Common Header Extraction / Injection errors
var (
	ErrInvalidSampledHeader      = errors.New("invalid B3 Sampled header found")
	ErrInvalidFlagsHeader        = errors.New("invalid B3 Flags header found")
	ErrInvalidTraceIDHeader      = errors.New("invalid B3 TraceID header found")
	ErrInvalidSpanIDHeader       = errors.New("invalid B3 SpanID header found")
	ErrInvalidParentSpanIDHeader = errors.New("invalid B3 ParentSpanID header found")
	ErrInvalidScope              = errors.New("require either both TraceID and SpanID or none")
	ErrInvalidScopeParent        = errors.New("ParentSpanID requires both TraceID and SpanID to be available")
	ErrEmptyContext              = errors.New("empty request context")
)

// Default B3 Header keys
const (
	TraceID      = "x-b3-traceid"
	SpanID       = "x-b3-spanid"
	ParentSpanID = "x-b3-parentspanid"
	Sampled      = "x-b3-sampled"
	Flags        = "x-b3-flags"
)
