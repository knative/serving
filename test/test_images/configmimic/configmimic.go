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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/signals"
)

var (
	priority = []string{
		os.Getenv("K_SERVICE"),
		os.Getenv("K_CONFIGURATION"),
		os.Getenv("K_REVISION"),
		cmp.Or(
			os.Getenv("POD_NAME"),
			os.Getenv("HOSTNAME"),
		),
	}

	ready   atomic.Bool
	live    atomic.Bool
	startup atomic.Bool
)

func handler(w http.ResponseWriter, _ *http.Request) {
	log.Println("Hello world received a request.")
	fmt.Fprintf(w, "Hello %v! How about some tasty noodles?", os.Getenv("NAME"))
}

func boolHandler(val *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if val.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}
}

func main() {
	ready.Store(false)
	live.Store(false)

	log.Println("Hello world app started.")

	checkForStatus()

	sig := signals.SetupSignalHandler()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/healthz/ready", boolHandler(&ready))
	mux.HandleFunc("/healthz/live", boolHandler(&live))
	mux.HandleFunc("/healthz/startup", boolHandler(&startup))

	server := pkgnet.NewServer(addr(), mux)

	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	for {
		select {
		case <-time.After(5 * time.Second):
			checkForStatus()
		case <-sig:
			return
		}
	}
}

func checkForStatus() {
	log.Println("Checking for status")

	var path string
	var states sets.Set[string]

	for _, entry := range priority {
		if entry == "" {
			continue
		}

		path = filepath.Join("/etc/config", entry)
		bytes, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			log.Printf("failed to read file at %q: %s\n", path, err)
			continue
		}

		states = sets.New(strings.Split(string(bytes), ",")...)
	}

	log.Println("Read state", filepath.Base(path), sets.List(states))

	newReady := states.Has("ready")
	oldReady := ready.Swap(newReady)
	if oldReady != newReady {
		log.Println("ready is now:", newReady)
	}

	newLive := states.Has("live")
	oldLive := live.Swap(newLive)
	if oldLive != newLive {
		log.Println("live is now:", newLive)
	}

	newStartup := states.Has("startup")
	oldStartup := startup.Swap(newStartup)
	if oldStartup != newStartup {
		log.Println("startup is now:", newLive)
	}

	if states.Has("fail") {
		log.Fatalf("you want me to fail")
	}
}

func addr() string {
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port

	}
	return ":8080"
}
