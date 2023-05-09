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
	"crypto/tls"
	"net/http"
)

// NewProxyAutoTLSTransport is same with NewProxyAutoTransport but it has tls.Config to create HTTPS request.
func NewProxyAutoTLSTransport(maxIdle, maxIdlePerHost int, tlsConf *tls.Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	//transport.DialContext = DialWithBackOff  // go documentation indicates that setting DialContext result in TLSClientConfig being ignored
	transport.DisableKeepAlives = false
	transport.MaxIdleConns = maxIdle
	transport.MaxIdleConnsPerHost = maxIdlePerHost
	transport.ForceAttemptHTTP2 = false
	transport.DisableCompression = true

	transport.TLSClientConfig = tlsConf

	return transport
}
