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
	"testing"
	"time"

	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/signals"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/helpers"
	"knative.dev/pkg/test/logstream"

	// For our e2e testing, we want this linked first so that our
	// systen namespace environment variable is defaulted prior to
	// logstream initialization.
	_ "knative.dev/networking/test/defaultsystem"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	// PollInterval is how frequently e2e tests will poll for updates.
	PollInterval = 1 * time.Second
	// PollTimeout is how long e2e tests will wait for resource updates when polling.
	PollTimeout = 10 * time.Minute
)

// util.go provides shared utilities methods across knative serving test

// Setup creates client to run Knative Service requests
func Setup(t testing.TB) *Clients {
	t.Helper()

	cancel := logstream.Start(t)
	t.Cleanup(cancel)

	cfg, err := pkgTest.Flags.GetRESTConfig()
	if err != nil {
		t.Fatal("couldn't get REST config:", err)
	}

	clients, err := NewClientsFromConfig(cfg, ServingNamespace)
	if err != nil {
		t.Fatal("Couldn't initialize clients", "error", err.Error())
	}
	return clients
}

// ObjectNameForTest generates a random object name based on the test name.
var ObjectNameForTest = helpers.ObjectNameForTest

// ListenAndServeGracefully calls into ListenAndServeGracefullyWithHandler
// by passing handler to handle requests for "/"
func ListenAndServeGracefully(addr string, handler func(w http.ResponseWriter, r *http.Request)) {
	ListenAndServeGracefullyWithHandler(addr, http.HandlerFunc(handler))
}

// ListenAndServeGracefullyWithHandler creates an HTTP server, listens on the defined address
// and handles incoming requests with the given handler.
// It blocks until SIGTERM is received and the underlying server has shutdown gracefully.
func ListenAndServeGracefullyWithHandler(addr string, handler http.Handler) {
	server := pkgnet.NewServer(addr, handler)
	go server.ListenAndServe()

	<-signals.SetupSignalHandler()
	server.Shutdown(context.Background())
}

// ListenAndServeTLSGracefully calls into ListenAndServeTLSGracefullyWithHandler
// by passing handler to handle requests for "/"
func ListenAndServeTLSGracefully(cert, key, addr string, handler func(w http.ResponseWriter, r *http.Request)) {
	ListenAndServeTLSGracefullyWithHandler(cert, key, addr, http.HandlerFunc(handler))
}

// ListenAndServeTLSGracefullyWithHandler creates an HTTPS server, listens on the defined address
// and handles incoming requests with the given handler.
// It blocks until SIGTERM is received and the underlying server has shutdown gracefully.
func ListenAndServeTLSGracefullyWithHandler(cert, key, addr string, handler http.Handler) {
	server := pkgnet.NewServer(addr, handler)
	go server.ListenAndServeTLS(cert, key)

	<-signals.SetupSignalHandler()
	server.Shutdown(context.Background())
}
