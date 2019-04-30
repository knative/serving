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
	"github.com/knative/serving/test"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("External connectivity received a request.")
	resp, err := http.Get("http://google.com")
	if err != nil {
		fmt.Fprintln(w, "Failure to access the outside world.")
		return
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
        fmt.Fprintln(w, "Hello! We are connected to the outside world.")
    } else {
        fmt.Fprintln(w, "Failure to access the outside world.")
    }

	defer resp.Body.Close()
}

func main() {
	flag.Parse()
	log.Print("External connectivity app started.")

	test.ListenAndServeGracefully(":8080", handler)
}
