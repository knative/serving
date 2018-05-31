package h2c

import (
	"crypto/tls"
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

func NewTransport() http.RoundTripper {
	return &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(netw, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(netw, addr)
		},
	}
}
