/*
Copyright 2018 The Knative Authors

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
)

func main() {
	log.Print("hellohttp2 app started.")

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/healthz", healthzHandler)

	protocols := &http.Protocols{}
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	s := &http.Server{
		Addr:      ":" + os.Getenv("PORT"),
		Handler:   mux,
		Protocols: protocols,
	}
	log.Fatal(s.ListenAndServe())
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Request: proto=%s method=%s path=%s", r.Proto, r.Method, r.URL.Path)

	if res, ok := os.LookupEnv("RESPONSE"); ok {
		fmt.Fprint(w, res)
	} else {
		fmt.Fprintf(w, "proto=%s method=%s path=%s\n", r.Proto, r.Method, r.URL.Path)
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Health check: proto=%s method=%s path=%s", r.Proto, r.Method, r.URL.Path)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
