/*
Copyright 2021 The Knative Authors

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
	"sync"
	"time"

	"knative.dev/serving/test"
)

var (
	healthy bool
	mu      sync.Mutex
)

func main() {
	finish := make(chan bool)
	// HTTP/TCP Probe.
	healthy = true
	if env := os.Getenv("READY_DELAY"); env != "" {
		delay, err := time.ParseDuration(env)
		if err != nil {
			log.Fatal(err)
		}

		healthy = false
		go func() {
			time.Sleep(delay)
			mu.Lock()
			healthy = true
			mu.Unlock()
		}()
	}

	if env := os.Getenv("LISTEN_DELAY"); env != "" {
		delay, err := time.ParseDuration(env)
		if err != nil {
			log.Fatal(err)
		}

		time.Sleep(delay)
	}

	mainServer := http.NewServeMux()
	mainServer.HandleFunc("/", handleMain)

	probeServer := http.NewServeMux()
	probeServer.HandleFunc("/", handleHealthz)

	go func() {
		http.ListenAndServe(":8077", probeServer)
	}()

	go func() {
		http.ListenAndServe(":8080", mainServer)
	}()

	<-finish
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if !healthy {
		http.Error(w, "not healthy", http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, test.HelloWorldText)
}

func handleMain(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, test.HelloWorldText)
}
