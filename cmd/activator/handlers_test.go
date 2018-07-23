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
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/controller"
	"go.uber.org/zap"
)

type fakeActivator struct {
	endpoint  activator.Endpoint
	namespace string
	name      string
}

func newFakeActivator(namespace string, name string, server *httptest.Server) fakeActivator {
	url, _ := url.Parse(server.URL)
	host := url.Hostname()
	port, _ := strconv.Atoi(url.Port())

	return fakeActivator{
		endpoint:  activator.Endpoint{FQDN: host, Port: int32(port)},
		namespace: namespace,
		name:      name,
	}
}

func (fa fakeActivator) ActiveEndpoint(namespace, name string) (activator.Endpoint, activator.Status, error) {
	if namespace == fa.namespace && name == fa.name {
		return fa.endpoint, http.StatusOK, nil
	}

	return activator.Endpoint{}, http.StatusNotFound, errors.New("not found!")
}

func (fa fakeActivator) Shutdown() {
}

func TestActivationHandler(t *testing.T) {
	logger := zap.NewExample().Sugar()

	errMsg := func(msg string) string {
		return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "everything good!")
		}),
	)
	defer server.Close()

	act := newFakeActivator("real-namespace", "real-name", server)

	examples := []struct {
		label     string
		namespace string
		name      string
		wantBody  string
		wantCode  int
		wantErr   error
	}{
		{"active endpoint", "real-namespace", "real-name", "everything good!", http.StatusOK, nil},
		{"no active endpoint", "fake-namespace", "fake-name", errMsg("not found!"), http.StatusNotFound, nil},
		{"request error", "real-namespace", "real-name", "", http.StatusBadGateway, errors.New("request error!")},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Host != "" {
					t.Errorf("Unexpected request host. Want %q, got %q", "", r.Host)
				}

				if e.wantErr != nil {
					return nil, e.wantErr
				}

				return http.DefaultTransport.RoundTrip(r)
			})

			handler := newActivationHandler(act, rt, logger)

			resp := httptest.NewRecorder()

			req := httptest.NewRequest("POST", "http://example.com", nil)
			req.Header.Set(controller.GetRevisionHeaderNamespace(), e.namespace)
			req.Header.Set(controller.GetRevisionHeaderName(), e.name)

			handler.ServeHTTP(resp, req)

			if resp.Code != e.wantCode {
				t.Errorf("Unexpected response status. Want %d, got %d", e.wantCode, resp.Code)
			}

			gotBody, _ := ioutil.ReadAll(resp.Body)
			if string(gotBody) != e.wantBody {
				t.Errorf("Unexpected response body. Want %q, got %q", e.wantBody, gotBody)
			}
		})
	}
}

func TestUploadHandler(t *testing.T) {
	payload := "SAMPLE PAYLOAD"

	examples := []struct {
		label     string
		maxUpload int
		status    int
	}{
		{"under", len(payload) + 1, http.StatusOK},
		{"equal", len(payload), http.StatusOK},
		{"over", len(payload) - 1, http.StatusRequestEntityTooLarge},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b1, _ := ioutil.ReadAll(r.Body)
				r.Body.Close()

				b2, _ := ioutil.ReadAll(r.Body)
				r.Body.Close()

				if string(b1) != payload || string(b2) != payload {
					t.Errorf("Expected request body to be rereadable. Want %q, got %q and %q.", payload, b1, b2)
				}
			})
			handler := newUploadHandler(baseHandler, int64(e.maxUpload))

			resp := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "http://example.com", bytes.NewBufferString(payload))

			handler.ServeHTTP(resp, req)

			if resp.Code != e.status {
				t.Errorf("Unexpected response status for payload %q. Want %d, got %d", payload, e.status, resp.Code)
			}
		})
	}
}

type readCloser struct {
	io.Reader
	closed bool
}

func (rc *readCloser) Close() error {
	rc.closed = true

	return nil
}

func TestRewinder(t *testing.T) {
	str := "test string"
	rc := &readCloser{bytes.NewBufferString(str), false}
	rewinder := newRewinder(rc)

	b1, err := ioutil.ReadAll(rewinder)
	if err != nil {
		t.Errorf("Unexpected error reading b1: %v", err)
	}
	rewinder.Close()

	b2, err := ioutil.ReadAll(rewinder)
	if err != nil {
		t.Errorf("Unexpected error reading b2: %v", err)
	}
	rewinder.Close()

	if string(b1) != str {
		t.Errorf("Unexpected str b1. Want %q, got %q", str, b1)
	}

	if string(b2) != str {
		t.Errorf("Unexpected str b2. Want %q, got %q", str, b2)
	}

	if !rc.closed {
		t.Errorf("Expected ReadCloser to be closed")
	}
}
