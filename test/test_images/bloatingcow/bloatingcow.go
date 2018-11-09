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
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

func bloat(mb int) {
	if mb == 0 {
		return
	}
	fmt.Printf("Bloat %v Mb of memory.\n", mb)

	b := make([]byte, mb*1024*1024)
	b[0] = 1
	b[len(b)-1] = 1
}

func handler(w http.ResponseWriter, r *http.Request) {
	memoryInMB := r.URL.Query().Get("memory_in_mb")
	if memoryInMB != "" {
		if mb, err := strconv.Atoi(memoryInMB); err != nil {
			fmt.Fprintf(w, "cannot convert %s to int", memoryInMB)
			w.WriteHeader(http.StatusBadRequest)
			return
		} else {
			bloat(mb)
		}
	}

	log.Print("Moo!")
	fmt.Fprintln(w, "Moo!")
}

func main() {
	flag.Parse()
	log.Print("Bloating cow app started.")

	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
