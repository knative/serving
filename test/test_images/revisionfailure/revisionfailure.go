/*
Copyright 2024 The Knative Authors

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
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"knative.dev/serving/test"
)

func handler(w http.ResponseWriter, _ *http.Request) {
	log.Print("Hello world received a request.")
	fmt.Fprintf(w, "Hello %v! How about some tasty noodles?", os.Getenv("NAME"))
}

func main() {
	stop := make(chan struct{})

	flag.Parse()
	log.Println("Hello world app started.")

	checkForFailure()

	go func() {
		test.ListenAndServeGracefully(":8080", handler)
		close(stop)
	}()

	for {
		select {
		case <-time.After(5 * time.Second):
			checkForFailure()
		case <-stop:
			return
		}
	}
}

func checkForFailure() {
	log.Println("Checking for failure")

	entries, err := os.ReadDir("/etc/config")
	if err != nil {
		log.Fatal("failed to read failure list", err)
	}

	for _, e := range entries {
		if e.Name() == os.Getenv("K_REVISION") {
			log.Fatalf("you want me to fail")
		}
	}
}
