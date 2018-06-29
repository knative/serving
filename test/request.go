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

// request contains logic to make polling HTTP requests against an endpoint with optional host spoofing.

package test

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"go.opencensus.io/trace"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	requestInterval = 1 * time.Second
	requestTimeout  = 5 * time.Minute
)

func waitForRequestToDomainState(logger *zap.SugaredLogger, address string, spoofDomain string, retryableCodes []int, inState func(body string) (bool, error)) error {
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
					logger.Infof("Retrying for code %v", resp.StatusCode)
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
// and spoof domain in the request headers, otherwise it will make the request directly to domain.
// desc will be used to name the metric that is emitted to track how long it took for the domain to get into the state checked by inState.
// Commas in `desc` must be escaped.
func WaitForEndpointState(kubeClientset *kubernetes.Clientset, logger *zap.SugaredLogger, resolvableDomain bool, domain string, namespaceName string, routeName string, inState func(body string) (bool, error), desc string) error {
	metricName := fmt.Sprintf("WaitForEndpointState/%s", desc)
	_, span := trace.StartSpan(context.Background(), metricName)
	defer span.End()

	var endpoint, spoofDomain string

	// If the domain that the Route controller is configured to assign to Route.Status.Domain
	// (the domainSuffix) is not resolvable, we need to retrieve the IP of the endpoint and
	// spoof the Host in our requests.
	if !resolvableDomain {
		ingressName := "knative-ingressgateway"
		ingressNamespace := "istio-system"
		ingress, err := kubeClientset.CoreV1().Services(ingressNamespace).Get(ingressName, metav1.GetOptions{})
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

	logger.Infof("Wait for the endpoint to be up and handling requests")
	// TODO(#348): The ingress endpoint tends to return 503's and 404's
	return waitForRequestToDomainState(logger, endpoint, spoofDomain, []int{503, 404}, inState)
}
