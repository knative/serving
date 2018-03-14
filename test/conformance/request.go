/*
Copyright 2018 Google Inc. All Rights Reserved.
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

// request contains logic to poll an ingress endpoint with host spoofing.

package conformance

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	requestInterval = 1 * time.Second
	requestTimeout  = 1 * time.Minute
)

// WaitForIngressRequestToDomainState makes requests to address every requestInterval until
// timeout has passed, or inState returns `true` (indicating it is done) or
// returns an error. Requests are made with host spoofed in the `Host` header.
// Will retry when responses return the HTTP codes in retryableCodes.
func WaitForIngressRequestToHostState(address string, host string, retryableCodes []int, inState func(body string) (bool, error)) {
	h := http.Client{}
	req, err := http.NewRequest("GET", address, nil)
	Expect(err).NotTo(HaveOccurred())

	req.Host = host

	var body []byte
	err = wait.PollImmediate(requestInterval, requestTimeout, func() (bool, error) {
		resp, err := h.Do(req)
		Expect(err).NotTo(HaveOccurred())

		if resp.StatusCode != 200 {
			for _, code := range retryableCodes {
				if resp.StatusCode == code {
					fmt.Printf("Retrying for code %v\n", resp.StatusCode)
					return false, nil
				}
			}
			s := fmt.Sprintf("Status code %d was not a retriable code (%v)", resp.StatusCode, retryableCodes)
			return true, errors.New(s)
		}
		body, err = ioutil.ReadAll(resp.Body)
		return inState(string(body))
	})
	Expect(err).NotTo(HaveOccurred())
}
