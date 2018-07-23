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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/controller"
	"go.uber.org/zap"
)

// activationHandler will proxy a request to the active endpoint for the specified revision,
// using the provided transport
type activationHandler struct {
	activator activator.Activator
	logger    *zap.SugaredLogger
	transport http.RoundTripper
}

func newActivationHandler(a activator.Activator, rt http.RoundTripper, l *zap.SugaredLogger) http.Handler {
	return &activationHandler{activator: a, transport: rt, logger: l}
}

func (a *activationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := r.Header.Get(controller.GetRevisionHeaderNamespace())
	name := r.Header.Get(controller.GetRevisionHeaderName())

	endpoint, status, err := a.activator.ActiveEndpoint(namespace, name)
	if err != nil {
		msg := fmt.Sprintf("Error getting active endpoint: %v", err)

		a.logger.Errorf(msg)
		http.Error(w, msg, int(status))

		return
	}

	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", endpoint.FQDN, endpoint.Port),
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = a.transport

	// TODO: Clear the host to avoid 404's.
	// https://github.com/knative/serving/issues/964
	r.Host = ""

	proxy.ServeHTTP(w, r)
}

// uploadHandler wraps the provided handler with a request body that supports
// re-reading and prevents uploads larger than `maxUploadBytes`
type uploadHandler struct {
	http.Handler
	MaxUploadBytes int64
}

func newUploadHandler(h http.Handler, maxUploadBytes int64) http.Handler {
	return uploadHandler{
		Handler:        h,
		MaxUploadBytes: maxUploadBytes,
	}
}

func (h uploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > h.MaxUploadBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	// The request body cannot be read multiple times for retries.
	// The workaround is to clone the request body into a byte reader
	// so the body can be read multiple times.
	r.Body = newRewinder(r.Body)

	h.Handler.ServeHTTP(w, r)
}

// rewinder wraps a single-use `ReadCloser` into a `ReadCloser` that can be read multiple times
type rewinder struct {
	rc io.ReadCloser
	rs io.ReadSeeker
}

func newRewinder(rc io.ReadCloser) io.ReadCloser {
	return &rewinder{rc: rc}
}

func (r *rewinder) Read(b []byte) (int, error) {
	// On the first `Read()`, the contents of `rc` is read into a buffer `rs`.
	// This buffer is used for all subsequent reads
	if r.rs == nil {
		buf, err := ioutil.ReadAll(r.rc)
		if err != nil {
			return 0, err
		}
		r.rc.Close()

		r.rs = bytes.NewReader(buf)
	}

	return r.rs.Read(b)
}

func (r *rewinder) Close() error {
	// Rewind the buffer on `Close()` for the next call to `Read`
	r.rs.Seek(0, io.SeekStart)

	return nil
}
