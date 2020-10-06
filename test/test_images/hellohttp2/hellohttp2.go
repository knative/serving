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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	networkingpkg "knative.dev/networking/pkg"
	"knative.dev/pkg/network"
)

func httpWrapper() http.Handler {
	return networkingpkg.NewProbeHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 {
				log.Print("hellohttp2 received an http2 request.")
				fmt.Fprintln(w, "Hello, New World! How about donuts and coffee?")
			} else {
				log.Print("hellohttp2 received an HTTP 1.1 request.")
				w.WriteHeader(http.StatusUpgradeRequired)
			}
		}),
	)
}

func main() {
	flag.Parse()
	log.Print("hellohttp2 app started.")

	s := network.NewServer(":"+os.Getenv("PORT"), httpWrapper())
	log.Fatal(s.ListenAndServe())
}
