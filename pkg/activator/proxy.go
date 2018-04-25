package activator

import (
	"fmt"
	"net/http/httputil"
	"net/url"
)

type Proxy struct {
	proxyRequests <-chan *ProxyRequest
}

func NewProxy(proxyRequests <-chan *ProxyRequest) *Proxy {
	p := &Proxy{
		proxyRequests: proxyRequests,
	}
	go func() {
		for {
			req := <-p.proxyRequests
			go func() {
				target := &url.URL{
					// TODO: support https
					Scheme: "http",
					Host:   fmt.Sprintf("%s:%d", req.Endpoint.Ip, req.Endpoint.Port),
				}
				proxy := httputil.NewSingleHostReverseProxy(target)
				proxy.ServeHTTP(req.HttpRequest.W, req.HttpRequest.R)
			}()
		}
	}()
}
