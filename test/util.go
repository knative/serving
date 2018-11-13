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

// ListenAndServeGracefully creates an HTTP server, listens on the defined address
// and handles incoming requests to "/" with the given function.
// It blocks until SIGTERM is received and the underlying server has shutdown gracefully.
func ListenAndServeGracefully(addr string, handler func(w http.ResponseWriter, r *http.Request)) {
	m := http.NewServeMux()
	m.HandleFunc("/", handler)
	server := http.Server{Addr: addr, Handler: m}

	go server.ListenAndServe()

	sigTermChan := make(chan os.Signal)
	signal.Notify(sigTermChan, syscall.SIGTERM)
	<-sigTermChan
	server.Shutdown(context.Background())
}
