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
	"os"

	"net/http"
	"net/http/httputil"
	"net/url"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/test"
)

const (
	targetHostEnv  = "TARGET_HOST"
	gatewayHostEnv = "GATEWAY_HOST"
	portEnv        = "PORT" // Allow port to be customized / randomly assigned by tests

	defaultPort = "8080"
)

var (
	httpProxy *httputil.ReverseProxy
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("HTTP proxy received a request.")
	// Reverse proxy does not automatically reset the Host header.
	// We need to manually reset it.
	r.Host = getTargetHostEnv()
	httpProxy.ServeHTTP(w, r)
}

func getPort() string {
	value := os.Getenv(portEnv)
	if value == "" {
		return defaultPort
	}
	return value
}

func getTargetHostEnv() string {
	value := os.Getenv(targetHostEnv)
	if value == "" {
		log.Fatalf("No env %v provided.", targetHostEnv)
	}
	return value
}

func initialHTTPProxy(proxyURL string) *httputil.ReverseProxy {
	target, err := url.Parse(proxyURL)
	if err != nil {
		log.Fatal("Failed to parse url ", proxyURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Print("error reverse proxying request: ", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
	return proxy
}

func main() {
	flag.Parse()
	log.Print("HTTP Proxy app started.")

	targetHost := getTargetHostEnv()
	port := getPort()

	// Gateway is an optional value. It is used only when resolvable domain is not set
	// for external access test, as xip.io is flaky.
	// ref: https://github.com/knative/serving/issues/5389
	gateway := os.Getenv(gatewayHostEnv)
	if gateway != "" {
		targetHost = gateway
	}
	targetURL := fmt.Sprint("http://", targetHost)
	log.Print("target is " + targetURL)
	httpProxy = initialHTTPProxy(targetURL)

	address := fmt.Sprint(":", port)
	log.Print("Listening on address: ", address)
	// Handle forwarding requests which uses "K-Network-Hash" header.
	probeHandler := network.NewProbeHandler(http.HandlerFunc(handler)).ServeHTTP
	test.ListenAndServeGracefully(address, probeHandler)
}
