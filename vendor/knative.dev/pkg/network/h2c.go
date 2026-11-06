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

package network

import (
	"context"
	"net"
	"net/http"
	"time"
)

// NewServer returns a new HTTP Server with HTTP2 handler.
func NewServer(addr string, h http.Handler) *http.Server {
	h1s := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: time.Minute, // https://medium.com/a-journey-with-go/go-understand-and-mitigate-slowloris-attack-711c1b1403f6
		Protocols:         new(http.Protocols),
	}
	// Serve HTTP/1.1 and both cleartext (h2c) and TLS (h2) HTTP/2. Setting
	// Protocols explicitly disables net/http's automatic ALPN, so SetHTTP2 is
	// required for a TLS listener to advertise "h2"; it is harmless for h2c.
	h1s.Protocols.SetHTTP1(true)
	h1s.Protocols.SetUnencryptedHTTP2(true)
	h1s.Protocols.SetHTTP2(true)

	return h1s
}

// NewH2CTransport constructs a new H2C transport.
// That transport will reroute all HTTPS traffic to HTTP. This is
// to explicitly allow h2c (http2 without TLS) transport.
// See https://github.com/golang/go/issues/14141 for more details.
func NewH2CTransport() http.RoundTripper {
	return newH2CTransport(false)
}

func newH2CTransport(disableCompression bool) http.RoundTripper {
	t := &http.Transport{
		DisableCompression: disableCompression,
		DialContext: func(ctx context.Context, netw, addr string) (net.Conn, error) {
			return DialWithBackOff(ctx, netw, addr)
		},
		Protocols: new(http.Protocols),
	}
	// Serve unencrypted HTTP/2 (h2c)
	t.Protocols.SetUnencryptedHTTP2(true)
	return t
}

// newH2Transport constructs a neew H2 transport. That transport will handles HTTPS traffic
// with TLS config.
func newH2Transport(disableCompression bool, tlsContext DialTLSContextFunc) http.RoundTripper {
	t := &http.Transport{
		DisableCompression: disableCompression,
		// Request h2 ALPN; dialer applies it when NextProtos is unset.
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return tlsContext(withH2ALPN(ctx), network, addr)
		},
		Protocols: new(http.Protocols),
	}
	// Serve encrypted HTTP/2 (h2)
	t.Protocols.SetHTTP2(true)
	return t
}
