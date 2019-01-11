package b3

import (
	"strconv"
	"strings"

	"github.com/openzipkin/zipkin-go/model"
)

// ParseHeaders takes values found from B3 Headers and tries to reconstruct a
// SpanContext.
func ParseHeaders(
	hdrTraceID, hdrSpanID, hdrParentSpanID, hdrSampled, hdrFlags string,
) (*model.SpanContext, error) {
	var (
		err           error
		spanID        uint64
		requiredCount int
		sc            = &model.SpanContext{}
	)

	// correct values for an existing sampled header are "0" and "1".
	// For legacy support and  being lenient to other tracing implementations we
	// allow "true" and "false" as inputs for interop purposes.
	switch strings.ToLower(hdrSampled) {
	case "0", "false":
		sampled := false
		sc.Sampled = &sampled
	case "1", "true":
		sampled := true
		sc.Sampled = &sampled
	case "":
		// sc.Sampled = nil
	default:
		return nil, ErrInvalidSampledHeader
	}

	// The only accepted value for Flags is "1". This will set Debug to true. All
	// other values and omission of header will be ignored.
	if hdrFlags == "1" {
		sc.Debug = true
		sc.Sampled = nil
	}

	if hdrTraceID != "" {
		requiredCount++
		if sc.TraceID, err = model.TraceIDFromHex(hdrTraceID); err != nil {
			return nil, ErrInvalidTraceIDHeader
		}
	}

	if hdrSpanID != "" {
		requiredCount++
		if spanID, err = strconv.ParseUint(hdrSpanID, 16, 64); err != nil {
			return nil, ErrInvalidSpanIDHeader
		}
		sc.ID = model.ID(spanID)
	}

	if requiredCount != 0 && requiredCount != 2 {
		return nil, ErrInvalidScope
	}

	if hdrParentSpanID != "" {
		if requiredCount == 0 {
			return nil, ErrInvalidScopeParent
		}
		if spanID, err = strconv.ParseUint(hdrParentSpanID, 16, 64); err != nil {
			return nil, ErrInvalidParentSpanIDHeader
		}
		parentSpanID := model.ID(spanID)
		sc.ParentID = &parentSpanID
	}

	return sc, nil
}
