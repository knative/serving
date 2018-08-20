package h2c

import (
	"crypto/tls"
	"net"
	"net/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func NewServer(addr string, h http.Handler) *http.Server {
	h1s := &http.Server{
		Addr:    addr,
		Handler: h2c.NewHandler(h, &http2.Server{}),
	}

	return h1s
}

func ListenAndServe(addr string, h http.Handler) error {
	s := NewServer(addr, h)
	return s.ListenAndServe()
}

// NewTransport will reroute all https traffic to http. This is
// to explicitly allow h2c (http2 without TLS) transport.
// See https://github.com/golang/go/issues/14141 for more details.
var DefaultTransport http.RoundTripper = &http2.Transport{
	AllowHTTP: true,
	DialTLS: func(netw, addr string, cfg *tls.Config) (net.Conn, error) {
		return net.Dial(netw, addr)
	},
}
