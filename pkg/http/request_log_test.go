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
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knative.dev/serving/pkg/network"
)

var (
	defaultRevInfo = &RequestLogRevision{
		Name:          "rev",
		Namespace:     "ns",
		Service:       "svc",
		Configuration: "cfg",
		PodName:       "pn",
		PodIP:         "ip",
	}
	defaultInputGetter = RequestLogTemplateInputGetterFromRevision(defaultRevInfo)
	baseHandler        = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
)

func TestRequestLogHandler(t *testing.T) {
	tests := []struct {
		name                  string
		url                   string
		body                  string
		template              string
		want                  string
		wantErr               bool
		isProbe               bool
		enableProbeRequestLog bool
	}{{
		name:     "empty template",
		url:      "http://example.com/testpage",
		body:     "test",
		template: "",
		want:     "",
	}, {
		name:     "template with new line",
		url:      "http://example.com/testpage",
		body:     "test",
		template: "{{.Request.URL}}\n",
		want:     "http://example.com/testpage\n",
	}, {
		name:     "template without new line",
		url:      "http://example.com",
		body:     "test",
		template: "{{.Request.ContentLength}}",
		want:     "4\n",
	}, {
		name:     "invalid template",
		url:      "http://example.com",
		body:     "test",
		template: "{{}}",
		want:     "",
		wantErr:  true,
	}, {
		name:     "revision info",
		url:      "http://example.com",
		body:     "test",
		template: "{{.Revision.Name}}, {{.Revision.Namespace}}, {{.Revision.Service}}, {{.Revision.Configuration}}, {{.Revision.PodName}}, {{.Revision.PodIP}}",
		want:     "rev, ns, svc, cfg, pn, ip\n",
	}, {
		name:     "probe request and logging support disabled",
		url:      "http://example.com",
		body:     "test",
		template: "{{.Request.ContentLength}}",
		want:     "",
		isProbe:  true,
	}, {
		name:                  "probe request and logging support enabled",
		url:                   "http://example.com",
		body:                  "test",
		template:              "{{.Request.ContentLength}}",
		want:                  "4\n",
		isProbe:               true,
		enableProbeRequestLog: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf := bytes.NewBufferString("")
			handler, err := NewRequestLogHandler(
				baseHandler, buf, test.template, defaultInputGetter, test.enableProbeRequestLog)
			if test.wantErr != (err != nil) {
				t.Errorf("got %v, want error %v", err, test.wantErr)
			}

			if !test.wantErr {
				resp := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, test.url, bytes.NewBufferString(test.body))
				if test.isProbe {
					req.Header.Set(network.ProbeHeaderName, "activator")
				}
				handler.ServeHTTP(resp, req)

				got := buf.String()
				if got != test.want {
					t.Errorf("got '%v', want '%v'", got, test.want)
				}
			}
		})
	}
}

func TestPanickingHandler(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	buf := bytes.NewBufferString("")
	handler, err := NewRequestLogHandler(
		baseHandler, buf, "{{.Request.URL}}", defaultInputGetter, false)
	if err != nil {
		t.Errorf("got %v, want error: %v", err, false)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewBufferString("test"))
	defer func() {
		err := recover()
		if err == nil {
			t.Error("want ServeHTTP to panic, got nothing.")
		}

		got := buf.String()
		if want := "http://example.com\n"; got != want {
			t.Errorf("got '%v', want '%v'", got, want)
		}
	}()
	handler.ServeHTTP(resp, req)
}

func TestFailedTemplateExecution(t *testing.T) {
	buf := bytes.NewBufferString("")
	handler, err := NewRequestLogHandler(
		baseHandler, buf, "{{.Request.Something}}", defaultInputGetter, false)
	if err != nil {
		t.Errorf("got %v, wantErr %v, ", err, false)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com", bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	got := buf.String()
	if want := "Invalid request log template: "; !strings.HasPrefix(got, want) {
		t.Errorf("got: '%v', want: '%v'", got, want)
	}
}

func TestSetTemplate(t *testing.T) {
	url, body := "http://example.com/testpage", "test"
	tests := []struct {
		name     string
		template string
		want     string
		wantErr  bool
	}{{
		name:     "empty template 1",
		template: "",
		want:     "",
		wantErr:  false,
	}, {
		name:     "template with new line",
		template: "{{.Request.URL}}\n",
		want:     "http://example.com/testpage\n",
		wantErr:  false,
	}, {
		name:     "empty template 2",
		template: "",
		want:     "",
		wantErr:  false,
	}, {
		name:     "template without new line",
		template: "{{.Request.ContentLength}}",
		want:     "4\n",
		wantErr:  false,
	}, {
		name:     "empty template 3",
		template: "",
		want:     "",
		wantErr:  false,
	}, {
		name:     "invalid template",
		template: "{{}}",
		want:     "",
		wantErr:  true,
	}}

	buf := bytes.NewBufferString("")
	handler, err := NewRequestLogHandler(baseHandler, buf, "", defaultInputGetter, false)
	if err != nil {
		t.Fatalf("want: no error, got: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := handler.SetTemplate(test.template)
			if test.wantErr != (err != nil) {
				t.Errorf("got %v, want error %v", err, test.wantErr)
			}

			if !test.wantErr {
				buf.Reset()
				resp := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
				handler.ServeHTTP(resp, req)
				got := buf.String()
				if got != test.want {
					t.Errorf("got '%v', want '%v'", got, test.want)
				}
			}
		})
	}
}

func BenchmarkRequestLogHandlerNoTemplate(b *testing.B) {
	handler, err := NewRequestLogHandler(baseHandler, ioutil.Discard, "", defaultInputGetter, false)
	if err != nil {
		b.Fatalf("Failed to create handler: %v", err)
	}
	resp := httptest.NewRecorder()

	b.Run(fmt.Sprint("sequential"), func(b *testing.B) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run(fmt.Sprint("parallel"), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}

func BenchmarkRequestLogHandlerDefaultTemplate(b *testing.B) {
	// Taken from config-observability.yaml
	tpl := `{"httpRequest": {"requestMethod": "{{.Request.Method}}", "requestUrl": "{{js .Request.RequestURI}}", "requestSize": "{{.Request.ContentLength}}", "status": {{.Response.Code}}, "responseSize": "{{.Response.Size}}", "userAgent": "{{js .Request.UserAgent}}", "remoteIp": "{{js .Request.RemoteAddr}}", "serverIp": "{{.Revision.PodIP}}", "referer": "{{js .Request.Referer}}", "latency": "{{.Response.Latency}}s", "protocol": "{{.Request.Proto}}"}, "traceId": "{{index .Request.Header "X-B3-Traceid"}}"}`
	handler, err := NewRequestLogHandler(baseHandler, ioutil.Discard, tpl, defaultInputGetter, false)
	if err != nil {
		b.Fatalf("Failed to create handler: %v", err)
	}
	resp := httptest.NewRecorder()

	b.Run(fmt.Sprint("sequential"), func(b *testing.B) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run(fmt.Sprint("parallel"), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}
