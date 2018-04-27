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
					Host:   fmt.Sprintf("%s:%d", req.endpoint.ip, req.endpoint.port),
				}
				proxy := httputil.NewSingleHostReverseProxy(target)
				proxy.ServeHTTP(req.HttpRequest.w, req.HttpRequest.r)
			}()
		}
	}()
	return p
}
