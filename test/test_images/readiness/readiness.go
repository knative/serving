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
	"strconv"
	"sync"
	"time"

	"knative.dev/serving/test"
)

var (
	healthy bool
	mu      sync.Mutex
)

func main() {
	// Exec probe.
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 && args[0] == "probe" {
		execProbeMain()
	}

	// HTTP/TCP Probe.
	healthy = true
	var mu sync.Mutex
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
	mainServer.HandleFunc("/start-failing", handleStartFailing)

	probeServer := http.NewServeMux()
	probeServer.HandleFunc("/", handleHealthz)

	if env := os.Getenv("HEALTHCHECK_PORT"); env != "" {
		healthcheckPort, _ := strconv.Atoi(env)
		go func() {
			http.ListenAndServe(":"+strconv.Itoa(healthcheckPort), probeServer)
		}()
	} else {
		mainServer.HandleFunc("/healthz", handleHealthz)
	}

	mainServer.HandleFunc("/query", handleQuery)
	http.ListenAndServe(":8080", mainServer)
}

func execProbeMain() {
	resp, err := http.Get(os.ExpandEnv("http://localhost:$PORT/healthz"))
	if err != nil {
		log.Fatal("Failed to probe: ", err)
	}
	if resp.StatusCode > 299 {
		os.Exit(1)
	}
	os.Exit(0)
}

func handleStartFailing(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	healthy = false
	fmt.Fprint(w, "will now fail readiness")
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

func handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("probe") == "ok" {
		fmt.Fprint(w, test.HelloWorldText)
	}
	http.Error(w, "no query", http.StatusInternalServerError)
}

func handleMain(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, test.HelloWorldText)
}
