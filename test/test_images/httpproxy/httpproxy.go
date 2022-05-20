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
	"log"
	"os"

	"net/http"
	"net/http/httputil"
	"net/url"

	"knative.dev/serving/test"
)

const (
	targetHostEnv  = "TARGET_HOST"
	gatewayHostEnv = "GATEWAY_HOST"
	portEnv        = "PORT" // Allow port to be customized / randomly assigned by tests

	defaultPort = "8080"
)

func main() {
	flag.Parse()
	log.Print("HTTP Proxy app started.")

	// Fetch port from env or use default if not provided.
	port := os.Getenv(portEnv)
	if port == "" {
		port = defaultPort
	}

	// Fetch target host from env or fail if not provided.
	targetHost := os.Getenv(targetHostEnv)
	if targetHost == "" {
		log.Fatalf("No value for env var %q provided.", targetHostEnv)
	}

	routedHost := targetHost
	// gateway is an optional value. It is used only when resolvable domain is not set
	// for external access test, as services like sslip.io may be flaky.
	// ref: https://github.com/knative/serving/issues/5389
	gateway := os.Getenv(gatewayHostEnv)
	if gateway != "" {
		routedHost = gateway
	}

	target := &url.URL{
		Scheme: "http",
		Host:   routedHost,
	}
	log.Print("target is ", target)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Print("error reverse proxying request: ", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Print("HTTP proxy received a request.")
		// Reverse proxy does not automatically reset the Host header.
		// We need to manually reset it.
		r.Host = targetHost
		proxy.ServeHTTP(w, r)
	})

	address := ":" + port
	log.Print("Listening on address: ", address)
	test.ListenAndServeGracefully(address, handler)
}
