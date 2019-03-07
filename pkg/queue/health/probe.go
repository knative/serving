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

// HTTPProbe checks that an OPTIONS http call is answered correctly with
// a '200'.
func HTTPProbe(addr string, timeout time.Duration) error {
	req, err := http.NewRequest(http.MethodOptions, "http://"+addr, nil)
	if err != nil {
		return err
	}

	client := http.Client{
		Timeout: timeout,
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %v", res.StatusCode)
	}

	return nil
}
