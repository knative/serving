/*
Copyright 2018 Google LLC

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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("Search service received a request.")
	target := os.Getenv("TARGET")
	if target == "" {
		target = "NOT SPECIFIED"
	}
	fmt.Fprintf(w, "Login service TARGET env: %s!\n", target)
}

func main() {
	flag.Parse()
	log.Print("Search service sample started.")

	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
