// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jaeger // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/jaeger"

import (
	"encoding/base64"
	"fmt"
	"reflect"

	"github.com/jaegertracing/jaeger/thrift-gen/jaeger"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/idutils"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/tracetranslator"
)

var blankJaegerThriftSpan = new(jaeger.Span)

// ThriftToTraces transforms a Thrift trace batch into ptrace.Traces.
func ThriftToTraces(batches *jaeger.Batch) (ptrace.Traces, error) {
	traceData := ptrace.NewTraces()
	jProcess := batches.GetProcess()
	jSpans := batches.GetSpans()

	if jProcess == nil && len(jSpans) == 0 {
		return traceData, nil
	}

	rs := traceData.ResourceSpans().AppendEmpty()
	jThriftProcessToInternalResource(jProcess, rs.Resource())

	if len(jSpans) == 0 {
		return traceData, nil
	}

	jThriftSpansToInternal(jSpans, rs.ScopeSpans().AppendEmpty().Spans())

	return traceData, nil
}

func jThriftProcessToInternalResource(process *jaeger.Process, dest pcommon.Resource) {
	if process == nil {
		return
	}

	serviceName := process.GetServiceName()
	tags := process.GetTags()
	if serviceName == "" && tags == nil {
		return
	}

	attrs := dest.Attributes()
	attrs.Clear()
	if serviceName != "" {
		attrs.EnsureCapacity(len(tags) + 1)
		attrs.UpsertString(conventions.AttributeServiceName, serviceName)
	} else {
		attrs.EnsureCapacity(len(tags))
	}
	jThriftTagsToInternalAttributes(tags, attrs)

	// Handle special keys translations.
	translateHostnameAttr(attrs)
	translateJaegerVersionAttr(attrs)
}

func jThriftSpansToInternal(spans []*jaeger.Span, dest ptrace.SpanSlice) {
	if len(spans) == 0 {
		return
	}

	dest.EnsureCapacity(len(spans))
	for _, span := range spans {
		if span == nil || reflect.DeepEqual(span, blankJaegerThriftSpan) {
			continue
		}
		jThriftSpanToInternal(span, dest.AppendEmpty())
	}
}

func jThriftSpanToInternal(span *jaeger.Span, dest ptrace.Span) {
	dest.SetTraceID(idutils.UInt64ToTraceID(uint64(span.TraceIdHigh), uint64(span.TraceIdLow)))
	dest.SetSpanID(idutils.UInt64ToSpanID(uint64(span.SpanId)))
	dest.SetName(span.OperationName)
	dest.SetStartTimestamp(microsecondsToUnixNano(span.StartTime))
	dest.SetEndTimestamp(microsecondsToUnixNano(span.StartTime + span.Duration))

	parentSpanID := span.ParentSpanId
	if parentSpanID != 0 {
		dest.SetParentSpanID(idutils.UInt64ToSpanID(uint64(parentSpanID)))
	}

	attrs := dest.Attributes()
	attrs.EnsureCapacity(len(span.Tags))
	jThriftTagsToInternalAttributes(span.Tags, attrs)
	setInternalSpanStatus(attrs, dest.Status())
	if spanKindAttr, ok := attrs.Get(tracetranslator.TagSpanKind); ok {
		dest.SetKind(jSpanKindToInternal(spanKindAttr.StringVal()))
		attrs.Remove(tracetranslator.TagSpanKind)
	}

	// drop the attributes slice if all of them were replaced during translation
	if attrs.Len() == 0 {
		attrs.Clear()
	}

	jThriftLogsToSpanEvents(span.Logs, dest.Events())
	jThriftReferencesToSpanLinks(span.References, parentSpanID, dest.Links())
}

// jThriftTagsToInternalAttributes sets internal span links based on jaeger span references skipping excludeParentID
func jThriftTagsToInternalAttributes(tags []*jaeger.Tag, dest pcommon.Map) {
	for _, tag := range tags {
		switch tag.GetVType() {
		case jaeger.TagType_STRING:
			dest.UpsertString(tag.Key, tag.GetVStr())
		case jaeger.TagType_BOOL:
			dest.UpsertBool(tag.Key, tag.GetVBool())
		case jaeger.TagType_LONG:
			dest.UpsertInt(tag.Key, tag.GetVLong())
		case jaeger.TagType_DOUBLE:
			dest.UpsertDouble(tag.Key, tag.GetVDouble())
		case jaeger.TagType_BINARY:
			dest.UpsertString(tag.Key, base64.StdEncoding.EncodeToString(tag.GetVBinary()))
		default:
			dest.UpsertString(tag.Key, fmt.Sprintf("<Unknown Jaeger TagType %q>", tag.GetVType()))
		}
	}
}

func jThriftLogsToSpanEvents(logs []*jaeger.Log, dest ptrace.SpanEventSlice) {
	if len(logs) == 0 {
		return
	}

	dest.EnsureCapacity(len(logs))

	for _, log := range logs {
		event := dest.AppendEmpty()

		event.SetTimestamp(microsecondsToUnixNano(log.Timestamp))
		if len(log.Fields) == 0 {
			continue
		}

		attrs := event.Attributes()
		attrs.Clear()
		attrs.EnsureCapacity(len(log.Fields))
		jThriftTagsToInternalAttributes(log.Fields, attrs)
		if name, ok := attrs.Get(eventNameAttr); ok {
			event.SetName(name.StringVal())
			attrs.Remove(eventNameAttr)
		}
	}
}

func jThriftReferencesToSpanLinks(refs []*jaeger.SpanRef, excludeParentID int64, dest ptrace.SpanLinkSlice) {
	if len(refs) == 0 || len(refs) == 1 && refs[0].SpanId == excludeParentID && refs[0].RefType == jaeger.SpanRefType_CHILD_OF {
		return
	}

	dest.EnsureCapacity(len(refs))
	for _, ref := range refs {
		if ref.SpanId == excludeParentID && ref.RefType == jaeger.SpanRefType_CHILD_OF {
			continue
		}

		link := dest.AppendEmpty()
		link.SetTraceID(idutils.UInt64ToTraceID(uint64(ref.TraceIdHigh), uint64(ref.TraceIdLow)))
		link.SetSpanID(idutils.UInt64ToSpanID(uint64(ref.SpanId)))
	}
}

// microsecondsToUnixNano converts epoch microseconds to pcommon.Timestamp
func microsecondsToUnixNano(ms int64) pcommon.Timestamp {
	return pcommon.Timestamp(uint64(ms) * 1000)
}
