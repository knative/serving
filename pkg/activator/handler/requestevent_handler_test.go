/*
Copyright 2018 The Knative Authors
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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"
)

func TestRequestEventHandler(t *testing.T) {
	namespace := "testspace"
	revision := "testrevision"

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	reqChan := make(chan ReqEvent, 2)
	handler := NewRequestEventHandler(reqChan, baseHandler)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
	ctx := withRevID(context.Background(), types.NamespacedName{Namespace: namespace, Name: revision})
	handler.ServeHTTP(resp, req.WithContext(ctx))

	in := <-handler.ReqChan
	wantIn := ReqEvent{
		Key:       types.NamespacedName{Namespace: namespace, Name: revision},
		EventType: ReqIn,
	}

	if !cmp.Equal(wantIn, in) {
		t.Errorf("Unexpected event (-want +got): %s", cmp.Diff(wantIn, in))
	}

	out := <-handler.ReqChan
	wantOut := ReqEvent{
		Key:       wantIn.Key,
		EventType: ReqOut,
	}

	if !cmp.Equal(wantOut, out) {
		t.Errorf("Unexpected event (-want +got): %s", cmp.Diff(wantOut, out))
	}
}
