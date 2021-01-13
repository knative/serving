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

package util

import (
	"net/http"
	"net/http/httputil"

	"knative.dev/serving/pkg/activator"
)

var headersToRemove = []string{
	activator.RevisionHeaderName,
	activator.RevisionHeaderNamespace,
}

// NewHeaderPruningReverseProxy returns a httputil.ReverseProxy that proxies
// requests to the given targetHost.  The returned reverse proxy strips
// activator-specific headers that should not reach the user container.
func NewHeaderPruningReverseProxy(targetHost string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = targetHost

			// Copied from httputil.NewSingleHostReverseProxy.
			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}

			for _, h := range headersToRemove {
				req.Header.Del(h)
			}
		},
	}
}
