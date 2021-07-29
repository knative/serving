/*
Copyright 2019 The Knative Authors

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
	"path/filepath"
	"strings"

	"knative.dev/serving/test"
)

func handler(w http.ResponseWriter, r *http.Request) {
	base := filepath.Dir(test.HelloVolumePath)
	p := filepath.Join(base, r.URL.Path)
	if p == base {
		p = test.HelloVolumePath
	}
	if !strings.HasPrefix(p, base) {
		http.Error(w, "there is no escape", http.StatusBadRequest)
		return
	}
	content, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Print("Hello volume received a request: ", string(content))
	fmt.Fprintln(w, string(content))
}

func main() {
	flag.Parse()
	log.Print("Hello volume app started.")

	test.ListenAndServeGracefully(":8080", handler)
}
