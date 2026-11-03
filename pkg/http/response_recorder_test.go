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

package http

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type fakeResponseWriter struct{}

func (w *fakeResponseWriter) Header() http.Header         { return http.Header{"item1": []string{"value1"}} }
func (w *fakeResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *fakeResponseWriter) WriteHeader(code int)        {}
func (w *fakeResponseWriter) Flush()                      {}

type fakeHijackResponseWriter struct {
	fakeResponseWriter
	conn net.Conn
}

func (w *fakeHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	client, server := net.Pipe()
	w.conn = client
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}

var defaultHeader = http.Header{"item1": {"value1"}}

func TestResponseRecorder(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus int
		finalStatus   int
		writer        http.ResponseWriter
		hijack        bool
		writeSize     int
		wantStatus    int
		wantSize      int
	}{{
		name:          "no hijack",
		initialStatus: http.StatusAccepted,
		finalStatus:   http.StatusBadGateway,
		writer:        &fakeResponseWriter{},
		writeSize:     12,
		wantStatus:    http.StatusBadGateway,
		wantSize:      12,
	}, {
		name:          "failed hijack",
		initialStatus: http.StatusAccepted,
		finalStatus:   http.StatusBadGateway,
		writer:        &fakeResponseWriter{},
		hijack:        true,
		writeSize:     12,
		wantStatus:    http.StatusBadGateway,
		wantSize:      12,
	}, {
		name:          "successful hijack",
		initialStatus: http.StatusAccepted,
		finalStatus:   http.StatusBadGateway,
		writer:        &fakeHijackResponseWriter{},
		hijack:        true,
		writeSize:     12,
		wantStatus:    http.StatusAccepted,
		wantSize:      12,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rr := NewResponseRecorder(test.writer, test.initialStatus)
			if test.hijack {
				conn, _, _ := rr.Hijack()
				if conn != nil {
					defer conn.Close()
				}
				if writer, ok := test.writer.(*fakeHijackResponseWriter); ok && writer.conn != nil {
					defer writer.conn.Close()
				}
			}

			b := make([]byte, test.writeSize)
			rr.Write(b)
			rr.Flush()
			rr.WriteHeader(test.finalStatus)

			if got, want := rr.ResponseCode, test.wantStatus; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
			if got, want := rr.ResponseSize, test.wantSize; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
			if diff := cmp.Diff(rr.Header(), defaultHeader); diff != "" {
				t.Error("Headers are different (-want, +got) =", diff)
			}
		})
	}
}

func TestResponseRecorderInformationalResponse(t *testing.T) {
	var rr *ResponseRecorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rr = NewResponseRecorder(w, http.StatusOK)
		rr.Header().Set("Link", "</style.css>; rel=preload; as=style")
		rr.WriteHeader(http.StatusEarlyHints)
		rr.WriteHeader(http.StatusNotFound)
		rr.Write([]byte("not found"))
	}))

	resp, err := server.Client().Get(server.URL)
	if err != nil {
		server.Close()
		t.Fatal("Error making request:", err)
	}
	resp.Body.Close()
	// Close waits for the handler to return before rr is read.
	server.Close()

	if got, want := resp.StatusCode, http.StatusNotFound; got != want {
		t.Errorf("Client status code = %d, want %d", got, want)
	}
	if got, want := rr.ResponseCode, http.StatusNotFound; got != want {
		t.Errorf("Recorded status code = %d, want %d", got, want)
	}
}
