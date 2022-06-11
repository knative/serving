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
	"github.com/jaegertracing/jaeger/model"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/idutils"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/tracetranslator"
)

// ProtoFromTraces translates internal trace data into the Jaeger Proto for GRPC.
// Returns slice of translated Jaeger batches and error if translation failed.
func ProtoFromTraces(td ptrace.Traces) ([]*model.Batch, error) {
	resourceSpans := td.ResourceSpans()

	if resourceSpans.Len() == 0 {
		return nil, nil
	}

	batches := make([]*model.Batch, 0, resourceSpans.Len())
	for i := 0; i < resourceSpans.Len(); i++ {
		rs := resourceSpans.At(i)
		batch := resourceSpansToJaegerProto(rs)
		if batch != nil {
			batches = append(batches, batch)
		}
	}

	return batches, nil
}

func resourceSpansToJaegerProto(rs ptrace.ResourceSpans) *model.Batch {
	resource := rs.Resource()
	ilss := rs.ScopeSpans()

	if resource.Attributes().Len() == 0 && ilss.Len() == 0 {
		return nil
	}

	batch := &model.Batch{
		Process: resourceToJaegerProtoProcess(resource),
	}

	if ilss.Len() == 0 {
		return batch
	}

	// Approximate the number of the spans as the number of the spans in the first
	// instrumentation library info.
	jSpans := make([]*model.Span, 0, ilss.At(0).Spans().Len())

	for i := 0; i < ilss.Len(); i++ {
		ils := ilss.At(i)
		spans := ils.Spans()
		for j := 0; j < spans.Len(); j++ {
			span := spans.At(j)
			jSpan := spanToJaegerProto(span, ils.Scope())
			if jSpan != nil {
				jSpans = append(jSpans, jSpan)
			}
		}
	}

	batch.Spans = jSpans

	return batch
}

func resourceToJaegerProtoProcess(resource pcommon.Resource) *model.Process {
	process := &model.Process{}
	attrs := resource.Attributes()
	if attrs.Len() == 0 {
		process.ServiceName = tracetranslator.ResourceNoServiceName
		return process
	}
	attrsCount := attrs.Len()
	if serviceName, ok := attrs.Get(conventions.AttributeServiceName); ok {
		process.ServiceName = serviceName.StringVal()
		attrsCount--
	}
	if attrsCount == 0 {
		return process
	}

	tags := make([]model.KeyValue, 0, attrsCount)
	process.Tags = appendTagsFromResourceAttributes(tags, attrs)
	return process

}

func appendTagsFromResourceAttributes(dest []model.KeyValue, attrs pcommon.Map) []model.KeyValue {
	if attrs.Len() == 0 {
		return dest
	}

	attrs.Range(func(key string, attr pcommon.Value) bool {
		if key == conventions.AttributeServiceName {
			return true
		}
		dest = append(dest, attributeToJaegerProtoTag(key, attr))
		return true
	})
	return dest
}

func appendTagsFromAttributes(dest []model.KeyValue, attrs pcommon.Map) []model.KeyValue {
	if attrs.Len() == 0 {
		return dest
	}
	attrs.Range(func(key string, attr pcommon.Value) bool {
		dest = append(dest, attributeToJaegerProtoTag(key, attr))
		return true
	})
	return dest
}

func attributeToJaegerProtoTag(key string, attr pcommon.Value) model.KeyValue {
	tag := model.KeyValue{Key: key}
	switch attr.Type() {
	case pcommon.ValueTypeString:
		// Jaeger-to-Internal maps binary tags to string attributes and encodes them as
		// base64 strings. Blindingly attempting to decode base64 seems too much.
		tag.VType = model.ValueType_STRING
		tag.VStr = attr.StringVal()
	case pcommon.ValueTypeInt:
		tag.VType = model.ValueType_INT64
		tag.VInt64 = attr.IntVal()
	case pcommon.ValueTypeBool:
		tag.VType = model.ValueType_BOOL
		tag.VBool = attr.BoolVal()
	case pcommon.ValueTypeDouble:
		tag.VType = model.ValueType_FLOAT64
		tag.VFloat64 = attr.DoubleVal()
	case pcommon.ValueTypeMap, pcommon.ValueTypeSlice:
		tag.VType = model.ValueType_STRING
		tag.VStr = attr.AsString()
	}
	return tag
}

func spanToJaegerProto(span ptrace.Span, libraryTags pcommon.InstrumentationScope) *model.Span {
	traceID := traceIDToJaegerProto(span.TraceID())
	jReferences := makeJaegerProtoReferences(span.Links(), span.ParentSpanID(), traceID)

	startTime := span.StartTimestamp().AsTime()
	return &model.Span{
		TraceID:       traceID,
		SpanID:        spanIDToJaegerProto(span.SpanID()),
		OperationName: span.Name(),
		References:    jReferences,
		StartTime:     startTime,
		Duration:      span.EndTimestamp().AsTime().Sub(startTime),
		Tags:          getJaegerProtoSpanTags(span, libraryTags),
		Logs:          spanEventsToJaegerProtoLogs(span.Events()),
	}
}

func getJaegerProtoSpanTags(span ptrace.Span, scope pcommon.InstrumentationScope) []model.KeyValue {
	var spanKindTag, statusCodeTag, errorTag, statusMsgTag model.KeyValue
	var spanKindTagFound, statusCodeTagFound, errorTagFound, statusMsgTagFound bool

	libraryTags, libraryTagsFound := getTagsFromInstrumentationLibrary(scope)

	tagsCount := span.Attributes().Len() + len(libraryTags)

	spanKindTag, spanKindTagFound = getTagFromSpanKind(span.Kind())
	if spanKindTagFound {
		tagsCount++
	}
	status := span.Status()
	statusCodeTag, statusCodeTagFound = getTagFromStatusCode(status.Code())
	if statusCodeTagFound {
		tagsCount++
	}

	errorTag, errorTagFound = getErrorTagFromStatusCode(status.Code())
	if errorTagFound {
		tagsCount++
	}

	statusMsgTag, statusMsgTagFound = getTagFromStatusMsg(status.Message())
	if statusMsgTagFound {
		tagsCount++
	}

	traceStateTags, traceStateTagsFound := getTagsFromTraceState(span.TraceState())
	if traceStateTagsFound {
		tagsCount += len(traceStateTags)
	}

	if tagsCount == 0 {
		return nil
	}

	tags := make([]model.KeyValue, 0, tagsCount)
	if libraryTagsFound {
		tags = append(tags, libraryTags...)
	}
	tags = appendTagsFromAttributes(tags, span.Attributes())
	if spanKindTagFound {
		tags = append(tags, spanKindTag)
	}
	if statusCodeTagFound {
		tags = append(tags, statusCodeTag)
	}
	if errorTagFound {
		tags = append(tags, errorTag)
	}
	if statusMsgTagFound {
		tags = append(tags, statusMsgTag)
	}
	if traceStateTagsFound {
		tags = append(tags, traceStateTags...)
	}
	return tags
}

func traceIDToJaegerProto(traceID pcommon.TraceID) model.TraceID {
	traceIDHigh, traceIDLow := idutils.TraceIDToUInt64Pair(traceID)
	return model.TraceID{
		Low:  traceIDLow,
		High: traceIDHigh,
	}
}

func spanIDToJaegerProto(spanID pcommon.SpanID) model.SpanID {
	return model.SpanID(idutils.SpanIDToUInt64(spanID))
}

// makeJaegerProtoReferences constructs jaeger span references based on parent span ID and span links
func makeJaegerProtoReferences(links ptrace.SpanLinkSlice, parentSpanID pcommon.SpanID, traceID model.TraceID) []model.SpanRef {
	parentSpanIDSet := !parentSpanID.IsEmpty()
	if !parentSpanIDSet && links.Len() == 0 {
		return nil
	}

	refsCount := links.Len()
	if parentSpanIDSet {
		refsCount++
	}

	refs := make([]model.SpanRef, 0, refsCount)

	// Put parent span ID at the first place because usually backends look for it
	// as the first CHILD_OF item in the model.SpanRef slice.
	if parentSpanIDSet {
		refs = append(refs, model.SpanRef{
			TraceID: traceID,
			SpanID:  spanIDToJaegerProto(parentSpanID),
			RefType: model.SpanRefType_CHILD_OF,
		})
	}

	for i := 0; i < links.Len(); i++ {
		link := links.At(i)
		refs = append(refs, model.SpanRef{
			TraceID: traceIDToJaegerProto(link.TraceID()),
			SpanID:  spanIDToJaegerProto(link.SpanID()),

			// Since Jaeger RefType is not captured in internal data,
			// use SpanRefType_FOLLOWS_FROM by default.
			// SpanRefType_CHILD_OF supposed to be set only from parentSpanID.
			RefType: model.SpanRefType_FOLLOWS_FROM,
		})
	}

	return refs
}

func spanEventsToJaegerProtoLogs(events ptrace.SpanEventSlice) []model.Log {
	if events.Len() == 0 {
		return nil
	}

	logs := make([]model.Log, 0, events.Len())
	for i := 0; i < events.Len(); i++ {
		event := events.At(i)
		fields := make([]model.KeyValue, 0, event.Attributes().Len()+1)
		_, eventAttrFound := event.Attributes().Get(eventNameAttr)
		if event.Name() != "" && !eventAttrFound {
			fields = append(fields, model.KeyValue{
				Key:   eventNameAttr,
				VType: model.ValueType_STRING,
				VStr:  event.Name(),
			})
		}
		fields = appendTagsFromAttributes(fields, event.Attributes())
		logs = append(logs, model.Log{
			Timestamp: event.Timestamp().AsTime(),
			Fields:    fields,
		})
	}

	return logs
}

func getTagFromSpanKind(spanKind ptrace.SpanKind) (model.KeyValue, bool) {
	var tagStr string
	switch spanKind {
	case ptrace.SpanKindClient:
		tagStr = string(tracetranslator.OpenTracingSpanKindClient)
	case ptrace.SpanKindServer:
		tagStr = string(tracetranslator.OpenTracingSpanKindServer)
	case ptrace.SpanKindProducer:
		tagStr = string(tracetranslator.OpenTracingSpanKindProducer)
	case ptrace.SpanKindConsumer:
		tagStr = string(tracetranslator.OpenTracingSpanKindConsumer)
	case ptrace.SpanKindInternal:
		tagStr = string(tracetranslator.OpenTracingSpanKindInternal)
	default:
		return model.KeyValue{}, false
	}

	return model.KeyValue{
		Key:   tracetranslator.TagSpanKind,
		VType: model.ValueType_STRING,
		VStr:  tagStr,
	}, true
}

func getTagFromStatusCode(statusCode ptrace.StatusCode) (model.KeyValue, bool) {
	switch statusCode {
	case ptrace.StatusCodeError:
		return model.KeyValue{
			Key:   conventions.OtelStatusCode,
			VType: model.ValueType_STRING,
			VStr:  statusError,
		}, true
	case ptrace.StatusCodeOk:
		return model.KeyValue{
			Key:   conventions.OtelStatusCode,
			VType: model.ValueType_STRING,
			VStr:  statusOk,
		}, true
	}
	return model.KeyValue{}, false
}

func getErrorTagFromStatusCode(statusCode ptrace.StatusCode) (model.KeyValue, bool) {
	if statusCode == ptrace.StatusCodeError {
		return model.KeyValue{
			Key:   tracetranslator.TagError,
			VBool: true,
			VType: model.ValueType_BOOL,
		}, true
	}
	return model.KeyValue{}, false

}

func getTagFromStatusMsg(statusMsg string) (model.KeyValue, bool) {
	if statusMsg == "" {
		return model.KeyValue{}, false
	}
	return model.KeyValue{
		Key:   conventions.OtelStatusDescription,
		VStr:  statusMsg,
		VType: model.ValueType_STRING,
	}, true
}

func getTagsFromTraceState(traceState ptrace.TraceState) ([]model.KeyValue, bool) {
	keyValues := make([]model.KeyValue, 0)
	exists := traceState != ptrace.TraceStateEmpty
	if exists {
		// TODO Bring this inline with solution for jaegertracing/jaeger-client-java #702 once available
		kv := model.KeyValue{
			Key:   tracetranslator.TagW3CTraceState,
			VStr:  string(traceState),
			VType: model.ValueType_STRING,
		}
		keyValues = append(keyValues, kv)
	}
	return keyValues, exists
}

func getTagsFromInstrumentationLibrary(il pcommon.InstrumentationScope) ([]model.KeyValue, bool) {
	keyValues := make([]model.KeyValue, 0)
	if ilName := il.Name(); ilName != "" {
		kv := model.KeyValue{
			Key:   conventions.OtelLibraryName,
			VStr:  ilName,
			VType: model.ValueType_STRING,
		}
		keyValues = append(keyValues, kv)
	}
	if ilVersion := il.Version(); ilVersion != "" {
		kv := model.KeyValue{
			Key:   conventions.OtelLibraryVersion,
			VStr:  ilVersion,
			VType: model.ValueType_STRING,
		}
		keyValues = append(keyValues, kv)
	}

	return keyValues, true
}
