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
	"strconv"
	"strings"

	"github.com/jaegertracing/jaeger/model"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/idutils"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/occonventions"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/tracetranslator"
)

var blankJaegerProtoSpan = new(model.Span)

// ProtoToTraces converts multiple Jaeger proto batches to internal traces
func ProtoToTraces(batches []*model.Batch) (ptrace.Traces, error) {
	traceData := ptrace.NewTraces()
	if len(batches) == 0 {
		return traceData, nil
	}

	rss := traceData.ResourceSpans()
	rss.EnsureCapacity(len(batches))

	for _, batch := range batches {
		if batch.GetProcess() == nil && len(batch.GetSpans()) == 0 {
			continue
		}

		protoBatchToResourceSpans(*batch, rss.AppendEmpty())
	}

	return traceData, nil
}

func protoBatchToResourceSpans(batch model.Batch, dest ptrace.ResourceSpans) {
	jSpans := batch.GetSpans()

	jProcessToInternalResource(batch.GetProcess(), dest.Resource())

	if len(jSpans) == 0 {
		return
	}

	groupByLibrary := jSpansToInternal(jSpans)
	ilss := dest.ScopeSpans()
	for library, spans := range groupByLibrary {
		ils := ilss.AppendEmpty()
		if library.name != "" {
			ils.Scope().SetName(library.name)
			ils.Scope().SetVersion(library.version)
		}
		spans.MoveAndAppendTo(ils.Spans())
	}
}

func jProcessToInternalResource(process *model.Process, dest pcommon.Resource) {
	if process == nil || process.ServiceName == tracetranslator.ResourceNoServiceName {
		return
	}

	serviceName := process.ServiceName
	tags := process.Tags
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
	jTagsToInternalAttributes(tags, attrs)

	// Handle special keys translations.
	translateHostnameAttr(attrs)
	translateJaegerVersionAttr(attrs)
}

// translateHostnameAttr translates "hostname" atttribute
func translateHostnameAttr(attrs pcommon.Map) {
	hostname, hostnameFound := attrs.Get("hostname")
	_, convHostNameFound := attrs.Get(conventions.AttributeHostName)
	if hostnameFound && !convHostNameFound {
		attrs.Insert(conventions.AttributeHostName, hostname)
		attrs.Remove("hostname")
	}
}

// translateHostnameAttr translates "jaeger.version" atttribute
func translateJaegerVersionAttr(attrs pcommon.Map) {
	jaegerVersion, jaegerVersionFound := attrs.Get("jaeger.version")
	_, exporterVersionFound := attrs.Get(occonventions.AttributeExporterVersion)
	if jaegerVersionFound && !exporterVersionFound {
		attrs.InsertString(occonventions.AttributeExporterVersion, "Jaeger-"+jaegerVersion.StringVal())
		attrs.Remove("jaeger.version")
	}
}

func jSpansToInternal(spans []*model.Span) map[scope]ptrace.SpanSlice {
	spansByLibrary := make(map[scope]ptrace.SpanSlice)

	for _, span := range spans {
		if span == nil || reflect.DeepEqual(span, blankJaegerProtoSpan) {
			continue
		}
		jSpanToInternal(span, spansByLibrary)
	}
	return spansByLibrary
}

type scope struct {
	name, version string
}

func jSpanToInternal(span *model.Span, spansByLibrary map[scope]ptrace.SpanSlice) {
	il := getScope(span)
	ss, found := spansByLibrary[il]
	if !found {
		ss = ptrace.NewSpanSlice()
		spansByLibrary[il] = ss
	}

	dest := ss.AppendEmpty()
	dest.SetTraceID(idutils.UInt64ToTraceID(span.TraceID.High, span.TraceID.Low))
	dest.SetSpanID(idutils.UInt64ToSpanID(uint64(span.SpanID)))
	dest.SetName(span.OperationName)
	dest.SetStartTimestamp(pcommon.NewTimestampFromTime(span.StartTime))
	dest.SetEndTimestamp(pcommon.NewTimestampFromTime(span.StartTime.Add(span.Duration)))

	parentSpanID := span.ParentSpanID()
	if parentSpanID != model.SpanID(0) {
		dest.SetParentSpanID(idutils.UInt64ToSpanID(uint64(parentSpanID)))
	}

	attrs := dest.Attributes()
	attrs.EnsureCapacity(len(span.Tags))
	jTagsToInternalAttributes(span.Tags, attrs)
	setInternalSpanStatus(attrs, dest.Status())
	if spanKindAttr, ok := attrs.Get(tracetranslator.TagSpanKind); ok {
		dest.SetKind(jSpanKindToInternal(spanKindAttr.StringVal()))
		attrs.Remove(tracetranslator.TagSpanKind)
	}

	dest.SetTraceState(getTraceStateFromAttrs(attrs))

	// drop the attributes slice if all of them were replaced during translation
	if attrs.Len() == 0 {
		attrs.Clear()
	}

	jLogsToSpanEvents(span.Logs, dest.Events())
	jReferencesToSpanLinks(span.References, parentSpanID, dest.Links())
}

func jTagsToInternalAttributes(tags []model.KeyValue, dest pcommon.Map) {
	for _, tag := range tags {
		switch tag.GetVType() {
		case model.ValueType_STRING:
			dest.UpsertString(tag.Key, tag.GetVStr())
		case model.ValueType_BOOL:
			dest.UpsertBool(tag.Key, tag.GetVBool())
		case model.ValueType_INT64:
			dest.UpsertInt(tag.Key, tag.GetVInt64())
		case model.ValueType_FLOAT64:
			dest.UpsertDouble(tag.Key, tag.GetVFloat64())
		case model.ValueType_BINARY:
			dest.UpsertString(tag.Key, base64.StdEncoding.EncodeToString(tag.GetVBinary()))
		default:
			dest.UpsertString(tag.Key, fmt.Sprintf("<Unknown Jaeger TagType %q>", tag.GetVType()))
		}
	}
}

func setInternalSpanStatus(attrs pcommon.Map, dest ptrace.SpanStatus) {
	statusCode := ptrace.StatusCodeUnset
	statusMessage := ""
	statusExists := false

	if errorVal, ok := attrs.Get(tracetranslator.TagError); ok {
		if errorVal.BoolVal() {
			statusCode = ptrace.StatusCodeError
			attrs.Remove(tracetranslator.TagError)
			statusExists = true

			if desc, ok := extractStatusDescFromAttr(attrs); ok {
				statusMessage = desc
			} else if descAttr, ok := attrs.Get(tracetranslator.TagHTTPStatusMsg); ok {
				statusMessage = descAttr.StringVal()
			}
		}
	}

	if codeAttr, ok := attrs.Get(conventions.OtelStatusCode); ok {
		if !statusExists {
			// The error tag is the ultimate truth for a Jaeger spans' error
			// status. Only parse the otel.status_code tag if the error tag is
			// not set to true.
			statusExists = true
			switch strings.ToUpper(codeAttr.StringVal()) {
			case statusOk:
				statusCode = ptrace.StatusCodeOk
			case statusError:
				statusCode = ptrace.StatusCodeError
			}

			if desc, ok := extractStatusDescFromAttr(attrs); ok {
				statusMessage = desc
			}
		}
		// Regardless of error tag value, remove the otel.status_code tag. The
		// otel.status_message tag will have already been removed if
		// statusExists is true.
		attrs.Remove(conventions.OtelStatusCode)
	} else if httpCodeAttr, ok := attrs.Get(conventions.AttributeHTTPStatusCode); !statusExists && ok {
		// Fallback to introspecting if this span represents a failed HTTP
		// request or response, but again, only do so if the `error` tag was
		// not set to true and no explicit status was sent.
		if code, err := getStatusCodeFromHTTPStatusAttr(httpCodeAttr); err == nil {
			if code != ptrace.StatusCodeUnset {
				statusExists = true
				statusCode = code
			}

			if msgAttr, ok := attrs.Get(tracetranslator.TagHTTPStatusMsg); ok {
				statusMessage = msgAttr.StringVal()
			}
		}
	}

	if statusExists {
		dest.SetCode(statusCode)
		dest.SetMessage(statusMessage)
	}
}

// extractStatusDescFromAttr returns the OTel status description from attrs
// along with true if it is set. Otherwise, an empty string and false are
// returned. The OTel status description attribute is deleted from attrs in
// the process.
func extractStatusDescFromAttr(attrs pcommon.Map) (string, bool) {
	if msgAttr, ok := attrs.Get(conventions.OtelStatusDescription); ok {
		msg := msgAttr.StringVal()
		attrs.Remove(conventions.OtelStatusDescription)
		return msg, true
	}
	return "", false
}

// codeFromAttr returns the integer code value from attrVal. An error is
// returned if the code is not represented by an integer or string value in
// the attrVal or the value is outside the bounds of an int representation.
func codeFromAttr(attrVal pcommon.Value) (int64, error) {
	var val int64
	switch attrVal.Type() {
	case pcommon.ValueTypeInt:
		val = attrVal.IntVal()
	case pcommon.ValueTypeString:
		var err error
		val, err = strconv.ParseInt(attrVal.StringVal(), 10, 0)
		if err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("%w: %s", errType, attrVal.Type().String())
	}
	return val, nil
}

func getStatusCodeFromHTTPStatusAttr(attrVal pcommon.Value) (ptrace.StatusCode, error) {
	statusCode, err := codeFromAttr(attrVal)
	if err != nil {
		return ptrace.StatusCodeUnset, err
	}

	return tracetranslator.StatusCodeFromHTTP(statusCode), nil
}

func jSpanKindToInternal(spanKind string) ptrace.SpanKind {
	switch spanKind {
	case "client":
		return ptrace.SpanKindClient
	case "server":
		return ptrace.SpanKindServer
	case "producer":
		return ptrace.SpanKindProducer
	case "consumer":
		return ptrace.SpanKindConsumer
	case "internal":
		return ptrace.SpanKindInternal
	}
	return ptrace.SpanKindUnspecified
}

func jLogsToSpanEvents(logs []model.Log, dest ptrace.SpanEventSlice) {
	if len(logs) == 0 {
		return
	}

	dest.EnsureCapacity(len(logs))

	for i, log := range logs {
		var event ptrace.SpanEvent
		if dest.Len() > i {
			event = dest.At(i)
		} else {
			event = dest.AppendEmpty()
		}

		event.SetTimestamp(pcommon.NewTimestampFromTime(log.Timestamp))
		if len(log.Fields) == 0 {
			continue
		}

		attrs := event.Attributes()
		attrs.Clear()
		attrs.EnsureCapacity(len(log.Fields))
		jTagsToInternalAttributes(log.Fields, attrs)
		if name, ok := attrs.Get(eventNameAttr); ok {
			event.SetName(name.StringVal())
			attrs.Remove(eventNameAttr)
		}
	}
}

// jReferencesToSpanLinks sets internal span links based on jaeger span references skipping excludeParentID
func jReferencesToSpanLinks(refs []model.SpanRef, excludeParentID model.SpanID, dest ptrace.SpanLinkSlice) {
	if len(refs) == 0 || len(refs) == 1 && refs[0].SpanID == excludeParentID && refs[0].RefType == model.ChildOf {
		return
	}

	dest.EnsureCapacity(len(refs))
	for _, ref := range refs {
		if ref.SpanID == excludeParentID && ref.RefType == model.ChildOf {
			continue
		}

		link := dest.AppendEmpty()
		link.SetTraceID(idutils.UInt64ToTraceID(ref.TraceID.High, ref.TraceID.Low))
		link.SetSpanID(idutils.UInt64ToSpanID(uint64(ref.SpanID)))
	}
}

func getTraceStateFromAttrs(attrs pcommon.Map) ptrace.TraceState {
	traceState := ptrace.TraceStateEmpty
	// TODO Bring this inline with solution for jaegertracing/jaeger-client-java #702 once available
	if attr, ok := attrs.Get(tracetranslator.TagW3CTraceState); ok {
		traceState = ptrace.TraceState(attr.StringVal())
		attrs.Remove(tracetranslator.TagW3CTraceState)
	}
	return traceState
}

func getScope(span *model.Span) scope {
	il := scope{}
	if libraryName, ok := getAndDeleteTag(span, conventions.OtelLibraryName); ok {
		il.name = libraryName
		if libraryVersion, ok := getAndDeleteTag(span, conventions.OtelLibraryVersion); ok {
			il.version = libraryVersion
		}
	}
	return il
}

func getAndDeleteTag(span *model.Span, key string) (string, bool) {
	for i := range span.Tags {
		if span.Tags[i].Key == key {
			value := span.Tags[i].GetVStr()
			span.Tags = append(span.Tags[:i], span.Tags[i+1:]...)
			return value, true
		}
	}
	return "", false
}
