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

		if got := revIDFrom(r.Context()); got != revID {
			t.Errorf("revIDFrom() = %v, want %v", got, revID)
		}
	})

	handler := NewContextHandler(ctx, baseHandler)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
	req.Header.Set(activator.RevisionHeaderNamespace, revID.Namespace)
	req.Header.Set(activator.RevisionHeaderName, revID.Name)
	handler.ServeHTTP(resp, req)

	if got, want := resp.Code, http.StatusOK; got != want {
		t.Errorf("StatusCode = %d, want %d, body: %s", got, want, resp.Body.String())
	}
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

func errMsg(msg string) string {
	return fmt.Sprintf("Error getting active endpoint: %s\n", msg)
}
