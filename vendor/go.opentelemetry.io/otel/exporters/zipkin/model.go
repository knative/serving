// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package zipkin // import "go.opentelemetry.io/otel/exporters/zipkin"

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	zkmodel "github.com/openzipkin/zipkin-go/model"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv120 "go.opentelemetry.io/otel/semconv/v1.20.0"
	semconv121 "go.opentelemetry.io/otel/semconv/v1.21.0"
	semconv125 "go.opentelemetry.io/otel/semconv/v1.25.0"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	keyPeerHostname attribute.Key = "peer.hostname"
	keyPeerAddress  attribute.Key = "peer.address"
)

var defaultServiceName string

func init() {
	// fetch service.name from default resource for backup
	defaultResource := resource.Default()
	if value, exists := defaultResource.Set().Value(semconv.ServiceNameKey); exists {
		defaultServiceName = value.AsString()
	}
}

// SpanModels converts OpenTelemetry spans into Zipkin model spans.
// This is used for exporting to Zipkin compatible tracing services.
func SpanModels(batch []tracesdk.ReadOnlySpan) []zkmodel.SpanModel {
	models := make([]zkmodel.SpanModel, 0, len(batch))
	for _, data := range batch {
		models = append(models, toZipkinSpanModel(data))
	}
	return models
}

func getServiceName(attrs []attribute.KeyValue) string {
	for _, kv := range attrs {
		if kv.Key == semconv.ServiceNameKey {
			return kv.Value.AsString()
		}
	}

	return defaultServiceName
}

func toZipkinSpanModel(data tracesdk.ReadOnlySpan) zkmodel.SpanModel {
	return zkmodel.SpanModel{
		SpanContext: toZipkinSpanContext(data),
		Name:        data.Name(),
		Kind:        toZipkinKind(data.SpanKind()),
		Timestamp:   data.StartTime(),
		Duration:    data.EndTime().Sub(data.StartTime()),
		Shared:      false,
		LocalEndpoint: &zkmodel.Endpoint{
			ServiceName: getServiceName(data.Resource().Attributes()),
		},
		RemoteEndpoint: toZipkinRemoteEndpoint(data),
		Annotations:    toZipkinAnnotations(data.Events()),
		Tags:           toZipkinTags(data),
	}
}

func toZipkinSpanContext(data tracesdk.ReadOnlySpan) zkmodel.SpanContext {
	return zkmodel.SpanContext{
		TraceID:  toZipkinTraceID(data.SpanContext().TraceID()),
		ID:       toZipkinID(data.SpanContext().SpanID()),
		ParentID: toZipkinParentID(data.Parent().SpanID()),
		Debug:    false,
		Sampled:  nil,
		Err:      nil,
	}
}

func toZipkinTraceID(traceID trace.TraceID) zkmodel.TraceID {
	return zkmodel.TraceID{
		High: binary.BigEndian.Uint64(traceID[:8]),
		Low:  binary.BigEndian.Uint64(traceID[8:]),
	}
}

func toZipkinID(spanID trace.SpanID) zkmodel.ID {
	return zkmodel.ID(binary.BigEndian.Uint64(spanID[:]))
}

func toZipkinParentID(spanID trace.SpanID) *zkmodel.ID {
	if spanID.IsValid() {
		id := toZipkinID(spanID)
		return &id
	}
	return nil
}

func toZipkinKind(kind trace.SpanKind) zkmodel.Kind {
	switch kind {
	case trace.SpanKindUnspecified:
		return zkmodel.Undetermined
	case trace.SpanKindInternal:
		// The spec says we should set the kind to nil, but
		// the model does not allow that.
		return zkmodel.Undetermined
	case trace.SpanKindServer:
		return zkmodel.Server
	case trace.SpanKindClient:
		return zkmodel.Client
	case trace.SpanKindProducer:
		return zkmodel.Producer
	case trace.SpanKindConsumer:
		return zkmodel.Consumer
	}
	return zkmodel.Undetermined
}

func toZipkinAnnotations(events []tracesdk.Event) []zkmodel.Annotation {
	if len(events) == 0 {
		return nil
	}
	annotations := make([]zkmodel.Annotation, 0, len(events))
	for _, event := range events {
		value := event.Name
		if len(event.Attributes) > 0 {
			jsonString := attributesToJSONMapString(event.Attributes)
			if jsonString != "" {
				value = fmt.Sprintf("%s: %s", event.Name, jsonString)
			}
		}
		annotations = append(annotations, zkmodel.Annotation{
			Timestamp: event.Time,
			Value:     value,
		})
	}
	return annotations
}

func attributesToJSONMapString(attributes []attribute.KeyValue) string {
	m := make(map[string]interface{}, len(attributes))
	for _, a := range attributes {
		m[(string)(a.Key)] = a.Value.AsInterface()
	}
	// if an error happens, the result will be an empty string
	jsonBytes, _ := json.Marshal(m)
	return (string)(jsonBytes)
}

// attributeToStringPair serializes each attribute to a string pair.
func attributeToStringPair(kv attribute.KeyValue) (string, string) {
	switch kv.Value.Type() {
	// For slice attributes, serialize as JSON list string.
	case attribute.BOOLSLICE:
		data, _ := json.Marshal(kv.Value.AsBoolSlice())
		return (string)(kv.Key), (string)(data)
	case attribute.INT64SLICE:
		data, _ := json.Marshal(kv.Value.AsInt64Slice())
		return (string)(kv.Key), (string)(data)
	case attribute.FLOAT64SLICE:
		data, _ := json.Marshal(kv.Value.AsFloat64Slice())
		return (string)(kv.Key), (string)(data)
	case attribute.STRINGSLICE:
		data, _ := json.Marshal(kv.Value.AsStringSlice())
		return (string)(kv.Key), (string)(data)
	default:
		return (string)(kv.Key), kv.Value.Emit()
	}
}

// extraZipkinTagsLen is a count of tags that may be added to every outgoing span.
var extraZipkinTagsLen = len([]attribute.Key{
	semconv.OTelStatusCodeKey,
	semconv.OTelScopeNameKey,
	semconv.OTelScopeVersionKey,
})

func toZipkinTags(data tracesdk.ReadOnlySpan) map[string]string {
	attr := data.Attributes()
	resourceAttr := data.Resource().Attributes()
	m := make(map[string]string, len(attr)+len(resourceAttr)+extraZipkinTagsLen)
	for _, kv := range attr {
		k, v := attributeToStringPair(kv)
		m[k] = v
	}
	for _, kv := range resourceAttr {
		k, v := attributeToStringPair(kv)
		m[k] = v
	}

	if data.Status().Code != codes.Unset {
		// Zipkin expect to receive uppercase status values
		// rather than default capitalized ones.
		m[string(semconv.OTelStatusCodeKey)] = strings.ToUpper(data.Status().Code.String())
	}

	if data.Status().Code == codes.Error {
		m["error"] = data.Status().Description
	} else {
		delete(m, "error")
	}

	if is := data.InstrumentationScope(); is.Name != "" {
		m[string(semconv.OTelScopeNameKey)] = is.Name
		if is.Version != "" {
			m[string(semconv.OTelScopeVersionKey)] = is.Version
		}
	}

	if len(m) == 0 {
		return nil
	}

	return m
}

// Rank determines selection order for remote endpoint. See the specification
// https://github.com/open-telemetry/opentelemetry-specification/blob/v1.28.0/specification/trace/sdk_exporters/zipkin.md#otlp---zipkin
var remoteEndpointKeyRank = map[attribute.Key]int{
	semconv.PeerServiceKey:            1,
	semconv.ServerAddressKey:          2,
	semconv120.NetPeerNameKey:         3,
	semconv.NetworkPeerAddressKey:     4,
	semconv121.ServerSocketDomainKey:  5,
	semconv121.ServerSocketAddressKey: 6,
	semconv120.NetSockPeerNameKey:     7,
	semconv120.NetSockPeerAddrKey:     8,
	keyPeerHostname:                   9,
	keyPeerAddress:                    10,
	semconv125.DBNameKey:              11,
}

func toZipkinRemoteEndpoint(data tracesdk.ReadOnlySpan) *zkmodel.Endpoint {
	// Should be set only for client or producer kind
	if sk := data.SpanKind(); sk != trace.SpanKindClient && sk != trace.SpanKindProducer {
		return nil
	}

	attr := data.Attributes()
	var endpointAttr attribute.KeyValue
	for _, kv := range attr {
		rank, ok := remoteEndpointKeyRank[kv.Key]
		if !ok {
			continue
		}

		currentKeyRank, ok := remoteEndpointKeyRank[endpointAttr.Key]
		if ok && rank < currentKeyRank {
			endpointAttr = kv
		} else if !ok {
			endpointAttr = kv
		}
	}

	if endpointAttr.Key == "" {
		return nil
	}

	v := endpointAttr.Value.AsString()

	switch endpointAttr.Key {
	case semconv.NetworkPeerAddressKey:
		return remoteEndpointPeerIPWithPort(v, semconv.NetworkPeerPortKey, attr)
	case semconv121.ServerSocketAddressKey:
		return remoteEndpointPeerIPWithPort(v, semconv121.ServerSocketPortKey, attr)
	case semconv120.NetSockPeerAddrKey:
		return remoteEndpointPeerIPWithPort(v, semconv121.NetSockPeerPortKey, attr)
	}

	return &zkmodel.Endpoint{
		ServiceName: v,
	}
}

func remoteEndpointPeerIPWithPort(peerIP string, portKey attribute.Key, attrs []attribute.KeyValue) *zkmodel.Endpoint {
	ip := net.ParseIP(peerIP)
	if ip == nil {
		return nil
	}

	endpoint := &zkmodel.Endpoint{}
	// Determine if IPv4 or IPv6
	if ip.To4() != nil {
		endpoint.IPv4 = ip
	} else {
		endpoint.IPv6 = ip
	}

	for _, kv := range attrs {
		if kv.Key == portKey {
			port, _ := strconv.ParseUint(kv.Value.Emit(), 10, 16)
			endpoint.Port = uint16(port) // nolint: gosec  // Bit size of 16 checked above.
			return endpoint
		}
	}

	return endpoint
}
