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
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("Env vars test app received a request.")
	svc := os.Getenv("K_SERVICE")
	cfg := os.Getenv("K_CONFIGURATION")
	rev := os.Getenv("K_REVISION")
	fmt.Fprintf(w, "Here are our env vars service:%s - configuration:%s - revision:%s", svc, cfg, rev)
}

func main() {
	flag.Parse()
	log.Print("Env vars test app started.")

	m := http.NewServeMux()
	server := http.Server{Addr: ":8080", Handler: m}
	m.HandleFunc("/", handler)

	go server.ListenAndServe()

	sigTermChan := make(chan os.Signal)
	signal.Notify(sigTermChan, syscall.SIGTERM)
	<-sigTermChan
	server.Shutdown(context.Background())
}
