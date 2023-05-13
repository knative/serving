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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"golang.org/x/net/http2"
	pkgnet "knative.dev/pkg/network"
	activatorhandler "knative.dev/serving/pkg/activator/handler"
)

type dialer struct {
	conf *tls.Config
}

func (d *dialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return pkgnet.DialTLSWithBackOff(ctx, network, addr, d.conf)
}

// NewProxyAutoTLSTransport is same with NewProxyAutoTransport but it has tls.Config to create HTTPS request.
func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsConf *tls.Config) *activatorhandler.HibrydTransport {
	tc := dialer{conf: tlsConf}
	http1 := http.DefaultTransport.(*http.Transport).Clone()
	http1.DialTLSContext = tc.DialTLSContext
	http1.DisableKeepAlives = false
	http1.MaxIdleConns = maxIdle
	http1.MaxIdleConnsPerHost = maxIdlePerHost
	http1.ForceAttemptHTTP2 = false
	http1.DisableCompression = true

	http1.TLSClientConfig = tlsConf

	http2 := &http2.Transport{
		DisableCompression: true,
		DialTLSContext: func(ctx context.Context, netw, addr string, tlsConf *tls.Config) (net.Conn, error) {
			fmt.Println("\t Http2 Dial without VerifyConnection")
			return pkgnet.DialTLSWithBackOff(ctx, netw, addr, tlsConf)
		},
		TLSClientConfig: tlsConf,
	}

	return &activatorhandler.HibrydTransport{
		HTTP1: http1,
		HTTP2: http2,
	}
}

// HTTP
func NewProxyAutoTransport(maxIdle, maxIdlePerHost int) *activatorhandler.HibrydTransport {
	http1 := http.DefaultTransport.(*http.Transport).Clone()
	http1.DialContext = pkgnet.DialWithBackOff
	http1.DisableKeepAlives = true
	http1.MaxIdleConns = maxIdle
	http1.MaxIdleConnsPerHost = maxIdlePerHost
	http1.ForceAttemptHTTP2 = false
	http1.DisableCompression = false

	http2 := &http2.Transport{
		AllowHTTP:          true,
		DisableCompression: true,
		DialTLS: func(netw, addr string, _ *tls.Config) (net.Conn, error) {
			return pkgnet.DialWithBackOff(context.Background(),
				netw, addr)
		},
	}

	return &activatorhandler.HibrydTransport{
		HTTP1: http1,
		HTTP2: http2,
	}
}
