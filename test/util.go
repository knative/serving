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

package test

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/signals"
)

const (
	// PollInterval is how frequently e2e tests will poll for updates.
	PollInterval = 1 * time.Second
	// PollTimeout is how long e2e tests will wait for resource updates when polling.
	PollTimeout = 10 * time.Minute

	// HelloVolumePath is the path to the test volume.
	HelloVolumePath = "/hello/world"
)

// util.go provides shared utilities methods across knative serving test

// ListenAndServeGracefully calls into ListenAndServeGracefullyWithPattern
// by passing handler to handle requests for "/"
func ListenAndServeGracefully(addr string, handler func(w http.ResponseWriter, r *http.Request)) {
	ListenAndServeGracefullyWithHandler(addr, http.HandlerFunc(handler))
}

// ListenAndServeGracefullyWithPattern creates an HTTP server, listens on the defined address
// and handles incoming requests with the given handler.
// It blocks until SIGTERM is received and the underlying server has shutdown gracefully.
func ListenAndServeGracefullyWithHandler(addr string, handler http.Handler) {
	server := http.Server{Addr: addr, Handler: h2c.NewHandler(handler, &http2.Server{})}
	go server.ListenAndServe()

	<-signals.SetupSignalHandler()
	server.Shutdown(context.Background())
}

// TODO(dangerd): Remove this and use duck.CreateBytePatch after release-0.9
// CreateBytePatch is a helper function that creates the same content as
// CreatePatch, but returns in []byte format instead of JSONPatch.
func CreateBytePatch(before, after interface{}) ([]byte, error) {
	patch, err := duck.CreatePatch(before, after)
	if err != nil {
		return nil, err
	}
	return patch.MarshalJSON()
}
