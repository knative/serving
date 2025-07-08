/*
Copyright 2019 The Knative Authors

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

package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/metrics"
)

func TestRequestMetricHandler(t *testing.T) {
	testNamespace := "real-namespace"
	testRevName := "real-name"
	testPod := "testPod"

	tests := []struct {
		label       string
		baseHandler http.HandlerFunc
		newHeader   map[string]string
		wantCode    int
		wantPanic   bool
	}{
		{
			label: "normal response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			wantCode: http.StatusOK,
		},
		{
			label: "panic response",
			baseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				panic(errors.New("handler error"))
			}),
			wantCode:  http.StatusBadRequest,
			wantPanic: true,
		},
	}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			handler := NewMetricHandler(testPod, test.baseHandler)

			labeler := &otelhttp.Labeler{}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))

			if len(test.newHeader) != 0 {
				for k, v := range test.newHeader {
					req.Header.Add(k, v)
				}
			}

			rev := revision(testNamespace, testRevName)

			defer func() {
				err := recover()
				if test.wantPanic && err == nil {
					t.Error("Want ServeHTTP to panic, got nothing.")
				}

				if resp.Code != test.wantCode {
					t.Errorf("Response Status = %d,  want: %d", resp.Code, test.wantCode)
				}

				got := labeler.Get()

				want := []attribute.KeyValue{
					metrics.ServiceNameKey.With(rev.Labels[serving.ServiceLabelKey]),
					metrics.ConfigurationNameKey.With(rev.Labels[serving.ConfigurationLabelKey]),
					metrics.RevisionNameKey.With(rev.Name),
					metrics.K8sNamespaceKey.With(rev.Namespace),
					metrics.ActivatorKeyValue,
				}

				if diff := cmp.Diff(want, got, cmpOpts...); diff != "" {
					t.Error("unexpected attribute diff (-want +got): ", diff)
				}

			}()

			ctx := otelhttp.ContextWithLabeler(context.Background(), labeler)
			ctx = WithRevisionAndID(ctx, rev, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
			handler.ServeHTTP(resp, req.WithContext(ctx))
		})
	}
}

func BenchmarkMetricHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	reqCtx := WithRevisionAndID(
		context.Background(),
		revision(testNamespace, testRevName),
		types.NamespacedName{Namespace: testNamespace, Name: testRevName},
	)

	reqCtx = otelhttp.ContextWithLabeler(reqCtx, &otelhttp.Labeler{})

	handler := NewMetricHandler("benchPod", baseHandler)

	resp := httptest.NewRecorder()
	b.Run("sequential", func(b *testing.B) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(reqCtx)
		for b.Loop() {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(reqCtx)
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}
