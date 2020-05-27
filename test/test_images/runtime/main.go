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

package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"knative.dev/serving/test"
	"knative.dev/serving/test/test_images/runtime/handlers"
)

func main() {
	// We expect PORT to be defined in a Knative environment
	// and don't want to mask this failure in a test image.
	port, isSet := os.LookupEnv("PORT")
	if !isSet {
		log.Fatal("Environment variable PORT is not set.")
	}

	// This is an option for exec readiness probe test.
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 && args[0] == "probe" {
		url := "http://localhost:" + port
		if _, err := http.Get(url); err != nil {
			log.Fatal("Failed to probe ", err)
		}
		return
	}

	mux := http.NewServeMux()
	handlers.InitHandlers(mux)

	log.Print("Server starting on port ", port)
	test.ListenAndServeGracefullyWithHandler(":"+port, mux)
}
