package h2c

import (
	"crypto/tls"
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

// NewTransport will reroute all https traffic to http. This is
// to explicitly allow h2c (http2 without TLS) transport.
// See https://github.com/golang/go/issues/14141 for more details.
func NewTransport() http.RoundTripper {
	return &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(netw, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(netw, addr)
		},
	}
}