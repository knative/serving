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
	"fmt"
	"net"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// TCPProbe checks that a TCP socket to the address can be opened.
// Did not reuse k8s.io/kubernetes/pkg/probe/tcp to not create a dependency
// on klog.
func TCPProbe(addr string, socketTimeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, socketTimeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// HTTPProbe checks that HTTP connection can be established to the address.
// Did not reuse k8s.io/kubernetes/pkg/probe/tcp to not create a dependency
// on klog.
func HTTPProbe(url string, headers []corev1.HTTPHeader) error {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
		Timeout: 100 * time.Millisecond,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("Error constructing request %s", err.Error())
	}
	for _, header := range headers {
		req.Header.Add(header.Name, header.Value)
	}
	var res *http.Response
	res, _ = httpClient.Do(req)
	if res == nil {
		return fmt.Errorf("Response is nil")
	}

	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return fmt.Errorf("Response status code is %d", res.StatusCode)
	}
	return nil
}
