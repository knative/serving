/*
Copyright 2022 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package watch

import (
	"context"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/jaegerexporter"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.9.0"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type JaegerTraces struct {
	ObjectNamePrefixFilter string
}

func (o *JaegerTraces) WriteHistory(h GVRHistory) {
	graphRoots := o.processHistory(h)

	traceData := ptrace.NewTraces()
	rss := traceData.ResourceSpans()

	for _, root := range graphRoots {
		o.createSpan(root, root.uid, time.Now().UTC(), rss)
	}

	factory := jaegerexporter.NewFactory()
	cfg := factory.CreateDefaultConfig().(*jaegerexporter.Config)
	cfg.Endpoint = "localhost:14250"
	cfg.TLSSetting.Insecure = true

	params := componenttest.NewNopExporterCreateSettings()

	exp, err := factory.CreateTracesExporter(context.Background(), params, cfg)
	if err != nil {
		panic(err)
	}
	if err := exp.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		panic(err)
	}
	if err := exp.ConsumeTraces(context.Background(), traceData); err != nil {
		panic(err)
	}
	if err := exp.Shutdown(context.Background()); err != nil {
		panic(err)
	}
}

func (o *JaegerTraces) createSpan(n *node, traceid string, artificialEnd time.Time, rss ptrace.ResourceSpansSlice) {
	name := NameExtractorRegexp.FindString(n.name)
	rspans := rss.AppendEmpty()

	r := rspans.Resource()
	rattr := r.Attributes()
	rattr.UpsertString(conventions.AttributeServiceName, n.gvr)

	ssslice := rspans.ScopeSpans()
	scopespans := ssslice.AppendEmpty()
	span := scopespans.Spans().AppendEmpty()
	span.SetTraceID(uidToTraceID(traceid))
	span.SetSpanID(uidToSpanID(n.uid))
	span.SetName(name)
	span.SetKind(ptrace.SpanKindServer)

	attr := span.Attributes()
	attr.UpsertString("test-name", name)
	attr.UpsertString("test-class", ClassExtractorRegexp.FindString(n.name))
	attr.UpsertString("name", n.name)
	attr.UpsertString("gvr", n.gvr)

	if n.owneruid != "" {
		span.SetParentSpanID(uidToSpanID(n.owneruid))
	}

	// Default to an artificial end time
	readyTime := artificialEnd

	readyTimeString, obj, ok := getReadyTime(n.history)
	if ok {
		var err error
		readyTime, err = time.Parse(time.RFC3339, readyTimeString)
		if err != nil {
			panic(fmt.Sprint("error parsing ready time ", err))
		}
		span.Status().SetCode(ptrace.StatusCodeOk)
	} else {
		span.Status().SetCode(ptrace.StatusCodeError)
		fmt.Println("marking artifical end time since there's no ready time", n.gvr, name, artificialEnd.String())
	}

	span.SetStartTimestamp(pcommon.NewTimestampFromTime(obj.GetCreationTimestamp().UTC()))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(readyTime.UTC()))

	ses := span.Events()
	for _, event := range n.history.K8sEvents {
		se := ses.AppendEmpty()
		timestampString, _, _ := unstructured.NestedString(event.Object, "lastTimestamp")
		timestamp, err := time.Parse(time.RFC3339, timestampString)
		if err != nil {
			panic(fmt.Sprint("failed to parse event timestamp", err))
		}

		name, _, _ := unstructured.NestedString(event.Object, "reason")
		message, _, _ := unstructured.NestedString(event.Object, "message")
		component, _, _ := unstructured.NestedString(event.Object, "source", "component")

		se.SetName(name)
		se.Attributes().UpsertString("message", message)
		se.Attributes().UpsertString("source-component", component)
		se.SetTimestamp(pcommon.NewTimestampFromTime(timestamp.UTC()))
	}

	for _, child := range n.children {
		o.createSpan(child, traceid, readyTime.Add(-time.Millisecond), rss)
	}
}

func uidToTraceID(uid string) pcommon.TraceID {
	var h [16]byte
	f := fnv.New128a()
	_, _ = f.Write([]byte(uid))
	_ = f.Sum(h[:0])
	return pcommon.NewTraceID(h)
}

func uidToSpanID(uid string) pcommon.SpanID {
	var h [8]byte
	f := fnv.New64a()
	_, _ = f.Write([]byte(uid))
	_ = f.Sum(h[:0])
	return pcommon.NewSpanID(h)
}

var (
	NameExtractorRegexp  = regexp.MustCompile(`\d+-of-\d+`)
	ClassExtractorRegexp = regexp.MustCompile(`scale-\d+`)
)

type node struct {
	name     string
	gvr      string
	uid      string
	owneruid string
	rootuid  string
	history  *ObjectHistory

	children []*node
}

func (o *JaegerTraces) processHistory(h GVRHistory) map[types.UID]*node {
	uidToNode := make(map[types.UID]*node)
	roots := make(map[types.UID]*node)
	outstanding := make(map[types.UID]*ObjectHistory)

	for gvr, itemHistory := range h {
		gvrString := fmt.Sprintf("%s.%s.%s", gvr.Group, gvr.Version, gvr.Resource)

	inner:
		for name, history := range itemHistory {
			if !strings.HasPrefix(name, o.ObjectNamePrefixFilter) {
				continue
			}

			// This happens when we re-run tests and there are old events
			if len(history.WatchEvents) == 0 {
				continue
			}

			obj := history.WatchEvents[0].Object
			n := &node{history: history, gvr: gvrString, name: name, uid: string(obj.GetUID())}
			uidToNode[obj.GetUID()] = n

			switch len(obj.GetOwnerReferences()) {
			case 0:
				roots[obj.GetUID()] = n
				continue inner
			case 1:
				ref := obj.GetOwnerReferences()[0]
				n.owneruid = string(ref.UID)
				owner, ok := uidToNode[ref.UID]
				if ok {
					owner.children = append(owner.children, n)
				} else {
					outstanding[obj.GetUID()] = history
				}
			default:
				panic("unsupported multiple owner refs")
			}
		}
	}

	for {
		if len(outstanding) == 0 {
			break
		}

		foundLink := false
		for _, history := range outstanding {
			obj := history.WatchEvents[0].Object
			n, ok := uidToNode[obj.GetUID()]
			if !ok {
				panic("expected node to be present")
			}

			ref := obj.GetOwnerReferences()[0]
			owner, ok := uidToNode[ref.UID]
			if ok {
				foundLink = true
				owner.children = append(owner.children, n)
				delete(outstanding, obj.GetUID())
			}
		}

		if !foundLink {
			panic("orphaned object exists")
		}
	}

	return roots
}
