/*
Copyright 2020 The Knative Authors

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

	"knative.dev/serving/test"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Println("second sidecar container received a request.")
	fmt.Fprintln(w, "Yay!! multi-container works !!")
}

func main() {
	flag.Parse()
	log.Print("second sidecar container started")
	test.ListenAndServeGracefully(":8883", handler)
}
