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
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"

	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/rs/dnscache"
	"knative.dev/networking/pkg/http/probe"
	"knative.dev/networking/test"
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

	scheme := "http"
	if caCert := os.Getenv("CA_CERT"); caCert != "" {
		scheme = "https"
	}
	target := &url.URL{
		Scheme: scheme,
		Host:   routedHost,
	}
	log.Print("target is ", target)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = newDNSCachingTransport()
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
	// Handle forwarding requests which uses "K-Network-Hash" header.
	handler = probe.NewHandler(handler).ServeHTTP

	address := ":" + port

	if cert, key := os.Getenv("CERT"), os.Getenv("KEY"); cert != "" && key != "" {
		log.Print("Listening on address with TLS: ", address)
		test.ListenAndServeTLSGracefullyWithHandler(cert, key, ":"+port, handler)
	} else {
		log.Print("Listening on address: ", address)
		test.ListenAndServeGracefully(address, handler)
	}

}

// newDNSCachingTransport caches DNS lookups locally to avoid issues like
// dial tcp: lookup ingress-conformance-0-visibility-split-ijpryqql.serving-tests.svc.c1550209590.local on 10.96.0.10:53: dial udp 10.96.0.10:53: operation was canceled
func newDNSCachingTransport() http.RoundTripper {
	resolver := &dnscache.Resolver{}

	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = false
	t.MaxIdleConns = 1000
	t.MaxIdleConnsPerHost = 100

	t.DialContext = func(ctx context.Context, network string, addr string) (conn net.Conn, err error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := resolver.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		var dialer net.Dialer
		for _, ip := range ips {
			conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
			if err == nil {
				break
			}
		}
		return
	}

	if caCert := os.Getenv("CA_CERT"); caCert != "" {
		rootCAs, err := createRootCAs(caCert)
		if err != nil {
			log.Fatal(err)
		}

		t.TLSClientConfig = &tls.Config{
			RootCAs: rootCAs,
			// If SERVER_NAME is not set the empty value will make the
			// TLS client infer the ServerName from the hostname.
			ServerName: os.Getenv("SERVER_NAME"),
		}
	}

	return t
}

func createRootCAs(caCertFile string) (*x509.CertPool, error) {
	pemData, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, err
	}
	rootCAs, err := x509.SystemCertPool()
	if rootCAs == nil || err != nil {
		if err != nil {
			log.Printf("Failed to load cert poll from system: %v. Will create a new cert pool.", err)
		}
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(pemData) {
		return nil, errors.New("failed to add the certificate to the root CA")
	}
	return rootCAs, nil
}
