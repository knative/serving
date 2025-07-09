/*
Copyright 2025 The Knative Authors

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

package queue

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	netheader "knative.dev/networking/pkg/http/header"
)

func TestRouteTagHandler(t *testing.T) {
	cases := []struct {
		name               string
		tagHeader          string
		defaultRouteHeader string
		expectedTag        string
	}{{
		name:        "tag route",
		tagHeader:   "test-tag",
		expectedTag: "test-tag",
	}, {
		name:               "default route",
		defaultRouteHeader: "true",
		expectedTag:        defaultTagName,
	}, {
		name:               "undefined",
		tagHeader:          "test-tag",
		defaultRouteHeader: "true",
		expectedTag:        undefinedTagName,
	}, {
		name:               "disabled",
		tagHeader:          "",
		defaultRouteHeader: "",
		expectedTag:        disabledTagName,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noopHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {})
			h := NewRouteTagHandler(noopHandler)
			l := &otelhttp.Labeler{}
			ctx := otelhttp.ContextWithLabeler(context.Background(), l)
			resp := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				ctx,
				http.MethodPost,
				targetURI,
				bytes.NewBufferString("test"),
			)

			if tc.tagHeader != "" {
				req.Header.Set(netheader.RouteTagKey, tc.tagHeader)
			}
			if tc.defaultRouteHeader != "" {
				req.Header.Set(netheader.DefaultRouteKey, tc.defaultRouteHeader)
			}
			h.ServeHTTP(resp, req)

			attrs := l.Get()

			if tc.expectedTag == "" && len(attrs) > 0 {
				t.Fatalf("unexpected attributes %#v", attrs)
			} else if tc.expectedTag == "" {
				return
			}

			if len(attrs) != 1 {
				t.Errorf("unexpected extra attributes %#v", attrs)
			}

			got := string(attrs[0].Key)
			want := "kn.route.tag"

			if diff := cmp.Diff(want, got); diff != "" {
				t.Error("unexpected tag key (-want +got): ", diff)
			}

			got = attrs[0].Value.AsString()
			want = tc.expectedTag

			if diff := cmp.Diff(want, got); diff != "" {
				t.Error("unexpected tag value (-want +got): ", diff)
			}
		})
	}
}
