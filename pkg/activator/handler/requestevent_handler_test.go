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
	"time"

	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/network"
)

func TestRequestEventHandler(t *testing.T) {
	namespace := "testspace"
	revision := "testrevision"
	timeOffset := 1 * time.Millisecond

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		time.Sleep(timeOffset)
	})

	reqChan := make(chan network.ReqEvent, 2)
	handler := NewRequestEventHandler(reqChan, baseHandler)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString(""))
	key := types.NamespacedName{Namespace: namespace, Name: revision}
	ctx := util.WithRevID(context.Background(), key)
	handler.ServeHTTP(resp, req.WithContext(ctx))

	in := <-handler.reqChan
	if in.Key != key {
		t.Errorf("in.Key = %v, want %v", in.Key, key)
	}
	if in.Type != network.ReqIn {
		t.Errorf("in.Type = %v, want %v", in.Type, network.ReqIn)
	}
	if in.Time.IsZero() {
		t.Error("in.Time = Time{}, want something real")
	}

	out := <-handler.reqChan
	if out.Key != key {
		t.Errorf("out.Key = %v, want %v", in.Key, key)
	}
	if out.Type != network.ReqOut {
		t.Errorf("in.Type = %v, want %v", in.Type, network.ReqIn)
	}
	if out.Time.Sub(in.Time) < timeOffset {
		t.Errorf("out.Time.Sub(in.Time) = %v, want > %v", out.Time.Sub(in.Time), timeOffset)
	}
}
