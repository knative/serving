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

package autotls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgTest "knative.dev/pkg/test"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/network"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"
)

var dialBackoff = wait.Backoff{
	Duration: 50 * time.Millisecond,
	Factor:   1.4,
	Jitter:   0.1, // At most 10% jitter.
	Steps:    100,
	Cap:      10 * time.Second,
}

// CreateDialContext looks up the endpoint information to create a "dialer" for
// the provided Ingress' public ingress loas balancer.  It can be used to
// contact external-visibility services with an HTTP client via:
//
//	client := &http.Client{
//		Transport: &http.Transport{
//			DialContext: CreateDialContext(t, ing, clients),
//		},
//	}
func CreateDialContext(t *testing.T, ing *v1alpha1.Ingress, clients *test.Clients) func(context.Context, string, string) (net.Conn, error) {
	t.Helper()
	if ing.Status.PublicLoadBalancer == nil || len(ing.Status.PublicLoadBalancer.Ingress) < 1 {
		t.Fatal("Ingress does not have a public load balancer assigned.")
	}

	// TODO(mattmoor): I'm open to tricks that would let us cleanly test multiple
	// public load balancers or LBs with multiple ingresses (below), but want to
	// keep our simple tests simple, thus the [0]s...

	// We expect an ingress LB with the form foo.bar.svc.cluster.local (though
	// we aren't strictly sensitive to the suffix, this is just illustrative.
	internalDomain := ing.Status.PublicLoadBalancer.Ingress[0].DomainInternal
	parts := strings.SplitN(internalDomain, ".", 3)
	if len(parts) < 3 {
		t.Fatal("Too few parts in internal domain:", internalDomain)
	}
	name, namespace := parts[0], parts[1]

	svc, err := clients.KubeClient.Kube.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unable to retrieve Kubernetes service %s/%s: %v", namespace, name, err)
	}
	if len(svc.Status.LoadBalancer.Ingress) < 1 {
		t.Fatal("Service does not have any ingresses (not type LoadBalancer?).")
	}
	ingress := svc.Status.LoadBalancer.Ingress[0]
	dial := network.NewBackoffDialer(dialBackoff)
	return func(ctx context.Context, _ string, address string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		// Allow "ingressendpoint" flag to override the discovered ingress IP/hostname,
		// this is required in minikube-like environments.
		if pkgTest.Flags.IngressEndpoint != "" {
			return dial(ctx, "tcp", pkgTest.Flags.IngressEndpoint)
		}
		if ingress.IP != "" {
			return dial(ctx, "tcp", ingress.IP+":"+port)
		}
		if ingress.Hostname != "" {
			return dial(ctx, "tcp", ingress.Hostname+":"+port)
		}
		return nil, errors.New("service ingress does not contain dialing information")
	}
}

type requestOption func(*http.Request)
type responseExpectation func(response *http.Response) error

func runtimeRequest(t *testing.T, client *http.Client, url string, opts ...requestOption) *types.RuntimeInfo {
	return runtimeRequestWithExpectations(t, client, url,
		[]responseExpectation{statusCodeExpectation(sets.NewInt(http.StatusOK))},
		false,
		opts...)
}

// runtimeRequestWithExpectations attempts to make a request to url and return runtime information.
// If connection is successful only then it will validate all response expectations.
// If allowDialError is set to true then function will not fail if connection is a dial error.
func runtimeRequestWithExpectations(t *testing.T, client *http.Client, url string,
	responseExpectations []responseExpectation,
	allowDialError bool,
	opts ...requestOption) *types.RuntimeInfo {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Errorf("Error creating Request: %v", err)
		return nil
	}

	for _, opt := range opts {
		opt(req)
	}

	resp, err := client.Do(req)

	if err != nil {
		if !allowDialError || !isDialError(err) {
			t.Errorf("Error making GET request: %v", err)
		}
		return nil
	}

	defer resp.Body.Close()

	for _, e := range responseExpectations {
		if err := e(resp); err != nil {
			t.Errorf("Error meeting response expectations: %v", err)
			dumpResponse(t, resp)
			return nil
		}
	}

	if resp.StatusCode == http.StatusOK {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Errorf("Unable to read response body: %v", err)
			dumpResponse(t, resp)
			return nil
		}
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Errorf("Unable to parse runtime image's response payload: %v", err)
			return nil
		}
		return ri
	}
	return nil
}

func dumpResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	b, err := httputil.DumpResponse(resp, true)
	if err != nil {
		t.Errorf("Error dumping response: %v", err)
	}
	t.Log(string(b))
}

func statusCodeExpectation(statusCodes sets.Int) responseExpectation {
	return func(response *http.Response) error {
		if !statusCodes.Has(response.StatusCode) {
			return fmt.Errorf("got unexpected status: %d, expected %v", response.StatusCode, statusCodes)
		}
		return nil
	}
}

func isDialError(err error) bool {
	if err, ok := err.(*url.Error); ok {
		err, ok := err.Err.(*net.OpError)
		return ok && err.Op == "dial"
	}
	return false
}
