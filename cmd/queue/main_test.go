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

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
)

const wantHost = "a-better-host.com"

func TestHandler_ReqEvent(t *testing.T) {
	var httpHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(activator.RevisionHeaderName) != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.Header.Get(activator.RevisionHeaderNamespace) != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if got, want := r.Host, wantHost; got != want {
			t.Errorf("Host header = %q, want: %q", got, want)
		}
		if got, want := r.Header.Get(network.OriginalHostHeader), ""; got != want {
			t.Errorf("%s header was preserved", network.OriginalHostHeader)
		}

		w.WriteHeader(http.StatusOK)
	}

	server := httptest.NewServer(httpHandler)
	serverURL, _ := url.Parse(server.URL)

	defer server.Close()
	proxy := httputil.NewSingleHostReverseProxy(serverURL)

	params := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	breaker := queue.NewBreaker(params)
	reqChan := make(chan queue.ReqEvent, 10)
	h := handler(reqChan, breaker, proxy)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	// Verify the Original host header processing.
	req.Host = "nimporte.pas"
	req.Header.Set(network.OriginalHostHeader, wantHost)

	req.Header.Set(network.ProxyHeaderName, activator.Name)
	h(writer, req)
	select {
	case e := <-reqChan:
		if e.EventType != queue.ProxiedIn {
			t.Errorf("Want: %v, got: %v\n", queue.ReqIn, e.EventType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for an event to be intercepted")
	}
}

func TestProberHandler(t *testing.T) {
	defer logtesting.ClearAll()
	logger = logtesting.TestLogger(t)

	// All arguments are needed only for serving.
	h := handler(nil, nil, nil)

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	req.Header.Set(network.ProbeHeaderName, "test-probe")
	req.Header.Set(network.ProxyHeaderName, activator.Name)

	h(writer, req)

	// Should get 400.
	if got, want := writer.Code, http.StatusBadRequest; got != want {
		t.Errorf("Bad probe status = %v, want: %v", got, want)
	}
	if got, want := strings.TrimSpace(writer.Body.String()), fmt.Sprintf(badProbeTemplate, "test-probe"); got != want {
		// \r\n might be inserted, etc.
		t.Errorf("Bad probe body = %q, want: %q, diff: %s", got, want, cmp.Diff(got, want))
	}

	// Fix up the header.
	writer = httptest.NewRecorder()
	req.Header.Set(network.ProbeHeaderName, queue.Name)

	server := httptest.NewServer(http.HandlerFunc(h))
	defer server.Close()
	userTargetAddress = strings.TrimPrefix(server.URL, "http://")
	h(writer, req)

	// Should get 200.
	if got, want := writer.Code, http.StatusOK; got != want {
		t.Errorf("Good probe status = %v, want: %v", got, want)
	}

	// Body should be the `queue`.
	if got, want := strings.TrimSpace(writer.Body.String()), queue.Name; got != want {
		// \r\n might be inserted, etc.
		t.Errorf("Good probe body = %q, want: %q, diff: %s", got, want, cmp.Diff(got, want))
	}
}

func TestCreateVarLogLink(t *testing.T) {
	dir, err := ioutil.TempDir("", "TestCreateVarLogLink")
	if err != nil {
		t.Errorf("Failed to created temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	createVarLogLink("default", "service-7f97f9465b-5kkm5", "user-container", "knative-var-log", dir)

	source := path.Join(dir, "default_service-7f97f9465b-5kkm5_user-container")
	want := "../knative-var-log"
	got, err := os.Readlink(source)
	if err != nil {
		t.Errorf("Failed to read symlink: %v", err)
	}
	if got != want {
		t.Errorf("Incorrect symlink = %q, want %q, diff: %s", got, want, cmp.Diff(got, want))
	}
}
