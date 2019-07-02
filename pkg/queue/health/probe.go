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

package health

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// HTTPProbeConfigOptions holds the HTTP probe config options
type HTTPProbeConfigOptions struct {
	Timeout time.Duration
	*corev1.HTTPGetAction
}

// TCPProbeConfigOptions holds the TCP probe config options
type TCPProbeConfigOptions struct {
	SocketTimeout time.Duration
	Address       string
}

// TCPProbe checks that a TCP socket to the address can be opened.
// Did not reuse k8s.io/kubernetes/pkg/probe/tcp to not create a dependency
// on klog.
func TCPProbe(config TCPProbeConfigOptions) error {
	conn, err := net.DialTimeout("tcp", config.Address, config.SocketTimeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// HTTPProbe checks that HTTP connection can be established to the address.
func HTTPProbe(config HTTPProbeConfigOptions) error {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: config.Timeout,
	}
	url := fmt.Sprintf("%s://%s:%d%s", config.Scheme, config.Host, config.Port.IntValue(), config.Path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("Error constructing request %s", err.Error())
	}

	for _, header := range config.HTTPHeaders {
		req.Header.Add(header.Name, header.Value)
	}

	var res *http.Response
	res, err = httpClient.Do(req)
	if err != nil {
		return err
	}

	if !IsHTTPProbeReady(res) {
		return errors.New("http probe did not respond Ready")
	}

	return nil
}

func IsHTTPProbeReady(res *http.Response) bool {
	if res == nil {
		return false
	}

	// response status code between 200-399 indicates success
	return res.StatusCode >= 200 && res.StatusCode < 400
}
