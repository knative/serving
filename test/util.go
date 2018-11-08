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
// util.go provides shared utilities methods across knative serving test
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/knative/pkg/test/logging"
)

// LogResourceObject logs the resource object with the resource name and value
func LogResourceObject(logger *logging.BaseLogger, value ResourceObjects) {
	// Log the route object
	if resourceJSON, err := json.Marshal(value); err != nil {
		logger.Infof("Failed to create json from resource object: %v", err)
	} else {
		logger.Infof("resource %s", string(resourceJSON))
	}
}

// ImagePath is a helper function to prefix image name with repo and suffix with tag
func ImagePath(name string) string {
	return fmt.Sprintf("%s/%s:%s", ServingFlags.DockerRepo, name, ServingFlags.Tag)
}

// GracefulServer is an HTTP server, that handles SIGTERM signals gracefully.
type GracefulServer struct {
	wrapped http.Server
}

// NewGracefulServer creates an HTTP server, that's handling SIGTERM signals
// gracefully. Its `ListenEndServe` method should be the last call in a main
// function.
func NewGracefulServer(addr string, handler http.Handler) *GracefulServer {
	return &GracefulServer{http.Server{Addr: addr, Handler: handler}}
}

// ListenAndServe behaves the same as ListenAndServe of the plain http.Server
// but it also adds signal handling and blocks until SIGTERM is sent and the
// server is shutdown properly.
func (s *GracefulServer) ListenAndServe() {
	go s.wrapped.ListenAndServe()

	sigTermChan := make(chan os.Signal)
	signal.Notify(sigTermChan, syscall.SIGTERM)
	<-sigTermChan
	s.wrapped.Shutdown(context.Background())
}
