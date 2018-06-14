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

// request contains logic to make polling HTTP requests against an endpoint with optional host spoofing.

package test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	requestInterval = 1 * time.Second
	requestTimeout  = 1 * time.Minute
)

func waitForRequestToDomainState(address string, spoofDomain string, retryableCodes []int, inState func(body string) (bool, error)) error {
	h := http.Client{}
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return err
	}

	if spoofDomain != "" {
		req.Host = spoofDomain
	}

	var body []byte
	err = wait.PollImmediate(requestInterval, requestTimeout, func() (bool, error) {
		resp, err := h.Do(req)
		if err != nil {
			return true, err
		}

		if resp.StatusCode != 200 {
			for _, code := range retryableCodes {
				if resp.StatusCode == code {
					glog.Infof("Retrying for code %v\n", resp.StatusCode)
					return false, nil
				}
			}
			s := fmt.Sprintf("Status code %d was not a retriable code (%v)", resp.StatusCode, retryableCodes)
			return true, errors.New(s)
		}
		body, err = ioutil.ReadAll(resp.Body)
		return inState(string(body))
	})
	return err
}

// WaitForEndpointState will poll an endpoint until inState indicates the state is achieved. If resolvableDomain
// is false, it will use kubeClientset to look up the ingress (named based on routeName) in the namespace namespaceName,
// and spoof domain in the request heaers, otherwise it will make the request directly to domain.
func WaitForEndpointState(kubeClientset *kubernetes.Clientset, resolvableDomain bool, domain string, namespaceName string, routeName string, inState func(body string) (bool, error)) error {
	var endpoint, spoofDomain string

	// If the domain that the Route controller is configured to assign to Route.Status.Domain
	// (the domainSuffix) is not resolvable, we need to retrieve the IP of the endpoint and
	// spoof the Host in our requests.
	if !resolvableDomain {
		ingressName := routeName + "-ingress"
		ingress, err := kubeClientset.ExtensionsV1beta1().Ingresses(namespaceName).Get(ingressName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if ingress.Status.LoadBalancer.Ingress[0].IP == "" {
			return fmt.Errorf("Expected ingress loadbalancer IP for %s to be set, instead was empty", ingressName)
		}
		endpoint = fmt.Sprintf("http://%s", ingress.Status.LoadBalancer.Ingress[0].IP)
		spoofDomain = domain
	} else {
		// If the domain is resolvable, we can use it directly when we make requests
		endpoint = domain
	}

	// TODO(#348): The ingress endpoint tends to return 503's and 404's
	return waitForRequestToDomainState(endpoint, spoofDomain, []int{503, 404}, inState)
}
