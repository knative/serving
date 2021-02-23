/*
Copyright 2020 The Knative Authors

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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	network "knative.dev/pkg/network"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator"
)

func TestContextHandler(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()
	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevName}
	revision := revision(revID.Namespace, revID.Name)
	revisionInformer(ctx, revision)

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := revisionFrom(r.Context()); got != revision {
			t.Errorf("revisionFrom() = %v, want %v", got, revision)
		}

		if got := RevIDFrom(r.Context()); got != revID {
			t.Errorf("RevIDFrom() = %v, want %v", got, revID)
		}
	})

	handler := NewContextHandler(ctx, baseHandler)

	t.Run("with headers", func(t *testing.T) {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
		req.Header.Set(activator.RevisionHeaderNamespace, revID.Namespace)
		req.Header.Set(activator.RevisionHeaderName, revID.Name)
		handler.ServeHTTP(resp, req)

		if got, want := resp.Code, http.StatusOK; got != want {
			t.Errorf("StatusCode = %d, want %d, body: %s", got, want, resp.Body.String())
		}
	})

	t.Run("with host", func(t *testing.T) {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://"+network.GetServiceHostname(revID.Name, revID.Namespace), bytes.NewBufferString(""))
		handler.ServeHTTP(resp, req)

		if got, want := resp.Code, http.StatusOK; got != want {
			t.Errorf("StatusCode = %d, want %d, body: %s", got, want, resp.Body.String())
		}
	})
}

func TestContextHandlerError(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()
	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevName}
	revision := revision(revID.Namespace, revID.Name)
	revisionInformer(ctx, revision)

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	handler := NewContextHandler(ctx, baseHandler)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
	req.Header.Set(activator.RevisionHeaderNamespace, "foospace")
	req.Header.Set(activator.RevisionHeaderName, "fooname")
	handler.ServeHTTP(resp, req)

	if got, want := resp.Code, http.StatusNotFound; got != want {
		t.Errorf("StatusCode = %d, want %d", got, want)
	}

	if got, want := resp.Body.String(), errMsg(`revision.serving.knative.dev "fooname" not found`); got != want {
		t.Errorf("Body = %q, want %q", got, want)
	}
}

func BenchmarkContextHandler(b *testing.B) {
	tests := []struct {
		label        string
		revisionName string
	}{{
		label:        "context handler success",
		revisionName: testRevName,
	}, {
		label:        "context handler failure",
		revisionName: "fake",
	}}
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(&testing.T{})
	defer cancel()
	revision := revision(testNamespace, testRevName)
	revisionInformer(ctx, revision)

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	handler := NewContextHandler(ctx, baseHandler)
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, testNamespace)

	for _, test := range tests {
		req.Header.Set(activator.RevisionHeaderName, test.revisionName)
		b.Run(fmt.Sprintf("%s-sequential", test.label), func(b *testing.B) {
			resp := httptest.NewRecorder()
			for j := 0; j < b.N; j++ {
				handler.ServeHTTP(resp, req)
			}
		})
		b.Run(fmt.Sprintf("%s-parallel", test.label), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				resp := httptest.NewRecorder()
				for pb.Next() {
					handler.ServeHTTP(resp, req)
				}
			})
		})
	}
}

func errMsg(msg string) string {
	return fmt.Sprintf("Error getting active endpoint: %s\n", msg)
}
