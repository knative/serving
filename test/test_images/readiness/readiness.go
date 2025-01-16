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

const (
	defaultPort = "8080"
)

var (
	healthy         bool
	mu              sync.Mutex
	livenessCounter int
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

	log.Printf("Slept enough\n")
	mainServer := http.NewServeMux()
	mainServer.HandleFunc("/", handleMain)
	mainServer.HandleFunc("/start-failing", handleStartFailing)
	// When the same image is used for a sidecar container, it is possible to give it
	// a signal to start failing readiness. The request is sent to $FORWARD_PORT in the sidecar.
	mainServer.HandleFunc("/start-failing-sidecar", handleStartFailingSidecar)

	probeServer := http.NewServeMux()
	probeServer.HandleFunc("/readiness", handleReadiness)
	probeServer.HandleFunc("/liveness", handleLiveness)

	if healthcheckPort := os.Getenv("HEALTHCHECK_PORT"); healthcheckPort != "" {
		go func() {
			http.ListenAndServe(":"+healthcheckPort, probeServer)
		}()
	} else {
		mainServer.HandleFunc("/healthz/readiness", handleReadiness)
		mainServer.HandleFunc("/healthz/liveness", handleLiveness)
		mainServer.HandleFunc("/healthz/livenessCounter", handleLivenessCounter)
	}

	mainServer.HandleFunc("/query", handleQuery)
	log.Printf("Ready to serve\n")
	if err := http.ListenAndServe(":"+getServerPort(), mainServer); err != nil {
		log.Printf("ERRRR: %v", err)
	}
}

func execProbeMain() {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/healthz/readiness", getServerPort()))
	if err != nil {
		log.Fatal("Failed to probe: ", err)
	}
	resp.Body.Close()
	if resp.StatusCode > 299 {
		os.Exit(1)
		return
	}
	os.Exit(0)
}

func handleStartFailing(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	healthy = false
	fmt.Fprint(w, "will now fail readiness")
}

func handleStartFailingSidecar(_ http.ResponseWriter, _ *http.Request) {
	resp, err := http.Get(os.ExpandEnv("http://localhost:$FORWARD_PORT/start-failing"))
	if err != nil {
		log.Fatalf("GET to /start-failing failed: %v", err)
	}
	defer resp.Body.Close()
}

func handleReadiness(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	log.Printf("Handling readiness1\n")
	if !healthy {
		log.Printf("Handling readiness2\n")
		http.Error(w, "not healthy", http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, test.HelloWorldText)
}

func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	log.Printf("Handling liveness1\n")
	if !healthy {
		http.Error(w, "not healthy", http.StatusInternalServerError)
		return
	}
	log.Printf("Handling liveness2\n")
	livenessCounter++

	fmt.Fprint(w, livenessCounter)
}

func handleLivenessCounter(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	fmt.Fprint(w, livenessCounter)
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("probe") == "ok" {
		fmt.Fprint(w, test.HelloWorldText)
	}
	http.Error(w, "no query", http.StatusInternalServerError)
}

func handleMain(w http.ResponseWriter, _ *http.Request) {
	log.Printf("Main\n")
	fmt.Fprint(w, test.HelloWorldText)
}

func getServerPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return defaultPort
}
