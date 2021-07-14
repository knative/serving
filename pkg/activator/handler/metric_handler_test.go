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
	"strconv"
	"testing"

	"go.opencensus.io/resource"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/metrics/metricstest"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/serving/pkg/activator"
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

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
			if test.newHeader != nil && len(test.newHeader) != 0 {
				for k, v := range test.newHeader {
					req.Header.Add(k, v)
				}
			}

			rev := revision(testNamespace, testRevName)

			defer reset()
			defer func() {
				err := recover()
				if test.wantPanic && err == nil {
					t.Error("Want ServeHTTP to panic, got nothing.")
				}

				if resp.Code != test.wantCode {
					t.Errorf("Response Status = %d,  want: %d", resp.Code, test.wantCode)
				}

				labelCode := test.wantCode
				if test.wantPanic {
					labelCode = http.StatusInternalServerError
				}

				wantResource := &resource.Resource{
					Type: "knative_revision",
					Labels: map[string]string{
						metrics.LabelNamespaceName:     rev.Namespace,
						metrics.LabelServiceName:       rev.Labels[serving.ServiceLabelKey],
						metrics.LabelConfigurationName: rev.Labels[serving.ConfigurationLabelKey],
						metrics.LabelRevisionName:      rev.Name,
					},
				}
				wantTags := map[string]string{
					metrics.LabelPodName:           testPod,
					metrics.LabelContainerName:     activator.Name,
					metrics.LabelResponseCode:      strconv.Itoa(labelCode),
					metrics.LabelResponseCodeClass: strconv.Itoa(labelCode/100) + "xx",
				}

				metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric(requestCountM.Name(), 1, wantTags).WithResource(wantResource))
				metricstest.AssertMetricExistsRequiredOnly(t, responseTimeInMsecM.Name())
			}()

			reqCtx := WithRevisionAndID(context.Background(), rev, types.NamespacedName{Namespace: testNamespace, Name: testRevName})
			handler.ServeHTTP(resp, req.WithContext(reqCtx))
		})
	}
}

func reset() {
	metricstest.Unregister(requestConcurrencyM.Name(), requestCountM.Name(), responseTimeInMsecM.Name())
	register()
}

func BenchmarkMetricHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	reqCtx := WithRevisionAndID(context.Background(), revision(testNamespace, testRevName), types.NamespacedName{Namespace: testNamespace, Name: testRevName})

	handler := NewMetricHandler("benchPod", baseHandler)

	resp := httptest.NewRecorder()
	b.Run("sequential", func(b *testing.B) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(reqCtx)
		for j := 0; j < b.N; j++ {
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
