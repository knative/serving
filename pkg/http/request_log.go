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
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"
)

// RequestLogHandler implements an http.Handler that writes request logs
// and calls the next handler.
type RequestLogHandler struct {
	handler     http.Handler
	inputGetter RequestLogTemplateInputGetter
	writer      io.Writer
	templateMux sync.RWMutex
	templateStr string
	template    *template.Template
}

// RequestLogRevision provides revision related static information
// for the template execution.
type RequestLogRevision struct {
	Name          string
	Namespace     string
	Service       string
	Configuration string
	PodName       string
	PodIP         string
}

// RequestLogResponse provided response related information for the template execution.
type RequestLogResponse struct {
	Code    int
	Size    int
	Latency float64
}

// RequestLogTemplateInput is the wrapper struct that provides all
// necessary information for the template execution.
type RequestLogTemplateInput struct {
	Request  *http.Request
	Response *RequestLogResponse
	Revision *RequestLogRevision
}

// RequestLogTemplateInputGetter defines a function returning the input to pass to a request log writer.
type RequestLogTemplateInputGetter func(req *http.Request, resp *RequestLogResponse) *RequestLogTemplateInput

// RequestLogTemplateInputGetterFromRevision returns a func that forms a template input using a static
// revision information.
func RequestLogTemplateInputGetterFromRevision(rev *RequestLogRevision) RequestLogTemplateInputGetter {
	return func(req *http.Request, resp *RequestLogResponse) *RequestLogTemplateInput {
		return &RequestLogTemplateInput{
			Request:  req,
			Response: resp,
			Revision: rev,
		}
	}
}

// NewRequestLogHandler creates an http.Handler that logs request logs to an io.Writer.
func NewRequestLogHandler(h http.Handler, w io.Writer, templateStr string,
	inputGetter RequestLogTemplateInputGetter) (*RequestLogHandler, error) {
	reqHandler := &RequestLogHandler{
		handler:     h,
		writer:      w,
		inputGetter: inputGetter,
	}
	if err := reqHandler.SetTemplate(templateStr); err != nil {
		return nil, err
	}
	return reqHandler, nil
}

// SetTemplate sets the template to use for formatting request logs.
// Setting the template to an empty string turns of writing request logs.
func (h *RequestLogHandler) SetTemplate(templateStr string) error {
	var t *template.Template
	// If templateStr is empty, we will set the template to nil
	// and effectively disable request logs.
	if templateStr != "" {
		// Make sure that the template ends with a newline. Otherwise,
		// logging backends will not be able to parse entries separately.
		if !strings.HasSuffix(templateStr, "\n") {
			templateStr = templateStr + "\n"
		}
		var err error
		t, err = template.New("requestLog").Parse(templateStr)
		if err != nil {
			return err
		}
	}

	h.templateMux.Lock()
	defer h.templateMux.Unlock()
	h.templateStr = templateStr
	h.template = t
	return nil
}

func (h *RequestLogHandler) getTemplate() *template.Template {
	h.templateMux.RLock()
	defer h.templateMux.RUnlock()
	return h.template
}

func (h *RequestLogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := h.getTemplate()
	if t == nil {
		h.handler.ServeHTTP(w, r)
		return
	}

	rr := NewResponseRecorder(w, http.StatusOK)
	startTime := time.Now()
	defer func() {
		// If ServeHTTP panics, recover, record the failure and panic again.
		err := recover()
		latency := time.Since(startTime).Seconds()
		if err != nil {
			h.write(t, h.inputGetter(r, &RequestLogResponse{
				Code:    http.StatusInternalServerError,
				Latency: latency,
				Size:    0,
			}))
			panic(err)
		} else {
			h.write(t, h.inputGetter(r, &RequestLogResponse{
				Code:    rr.ResponseCode,
				Latency: latency,
				Size:    (int)(rr.ResponseSize),
			}))
		}
	}()
	h.handler.ServeHTTP(rr, r)
}

func (h *RequestLogHandler) write(t *template.Template, in *RequestLogTemplateInput) {
	if err := t.Execute(h.writer, in); err != nil {
		// Template execution failed. Write an error message with some basic information about the request.
		fmt.Fprintf(h.writer, "Invalid request log template: method: %v, response code: %v, latency: %v, url: %v\n",
			in.Request.Method, in.Response.Code, in.Response.Latency, in.Request.URL)
	}
}
