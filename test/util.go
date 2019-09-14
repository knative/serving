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

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
)

// HelloVolumePath is the path to the test volume.
const HelloVolumePath = "/hello/world"

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
	logger, _ := pkglogging.NewLogger(`{
              "level": "info",
              "development": false,
              "outputPaths": ["stdout"],
              "errorOutputPaths": ["stderr"],
              "encoding": "json",
              "encoderConfig": {
                "timeKey": "ts",
                "levelKey": "level",
                "nameKey": "logger",
                "callerKey": "caller",
                "messageKey": "msg",
                "stacktraceKey": "stacktrace",
                "lineEnding": "",
                "levelEncoder": "",
                "timeEncoder": "iso8601",
                "durationEncoder": "",
                "callerEncoder": ""
              }
            }`, "")

	server := http.Server{Addr: addr, Handler: h2c.NewHandler(handler, &http2.Server{})}
	go server.ListenAndServe()

	<-signals.SetupSignalHandler()
	logger.Info("Receiving SIGTERM. Attempting to gracefully shutdown server.")
	server.Shutdown(context.Background())
	logger.Info("Server shutdown.")
}
