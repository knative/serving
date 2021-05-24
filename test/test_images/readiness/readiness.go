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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"knative.dev/serving/test"
)

func main() {
	// Exec probe.
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 && args[0] == "probe" {
		if _, err := http.Get(os.ExpandEnv("http://localhost:$PORT/")); err != nil {
			log.Fatal("Failed to probe ", err)
		}
		return
	}

	// HTTP Probe.
	healthy := true
	var mu sync.Mutex
	if env := os.Getenv("STARTUP_DELAY"); env != "" {
		delay, err := time.ParseDuration(env)
		if err != nil {
			log.Fatal(err)
		}

		healthy = false
		go func() {
			<-time.After(delay)
			mu.Lock()
			healthy = true
			mu.Unlock()
		}()
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if !healthy {
			http.Error(w, "not healthy", http.StatusInternalServerError)
			return
		}

		fmt.Fprint(w, test.HelloWorldText)
	})

	http.ListenAndServe(":8080", nil)
}
