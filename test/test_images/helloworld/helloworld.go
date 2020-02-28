/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"net/http"
	"os"

	"go.uber.org/zap"
	"knative.dev/pkg/logging"
	"knative.dev/serving/test"
)

var logger *zap.SugaredLogger

func handler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Hello world received a request.")
	fmt.Fprintln(w, "Hello World! How about some tasty noodles?")
}

func main() {
	logger, _ = logging.NewLogger("", "debug")
	logger = logger.Named("user-container")
	defer flush(logger)

	test.ListenAndServeGracefully(":8080", handler)
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	os.Stdout.Sync()
	os.Stderr.Sync()
}
