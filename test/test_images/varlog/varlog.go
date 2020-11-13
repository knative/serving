/*
Copyright 2020 The Knative Authors

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
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"knative.dev/serving/test"
)

type handler struct {
	logger *log.Logger
}

func (h *handler) Handle(w http.ResponseWriter, r *http.Request) {
	h.logger.Println("var log received a request at " + time.Now().String())
	fmt.Fprintln(w, "var log! How about some tasty noodles?")
}

func main() {
	f, err := os.OpenFile("/var/log/server.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		panic(err)
	}

	defer f.Close()

	logger := log.New(f, "[server] ", log.LstdFlags)
	logger.Println("var log app started at " + time.Now().String())

	handler := handler{logger: logger}

	test.ListenAndServeGracefully(":8080", handler.Handle)
}
