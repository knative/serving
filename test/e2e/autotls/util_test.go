// +build e2e

/*
Copyright 2021 The Knative Authors

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
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"
)

type RequestOption func(*http.Request)
type ResponseExpectation func(response *http.Response) error

func RuntimeRequest(ctx context.Context, t *testing.T, client *http.Client, url string, opts ...RequestOption) *types.RuntimeInfo {
	return RuntimeRequestWithExpectations(ctx, t, client, url,
		[]ResponseExpectation{StatusCodeExpectation(sets.NewInt(http.StatusOK))},
		false,
		opts...)
}

// RuntimeRequestWithExpectations attempts to make a request to url and return runtime information.
// If connection is successful only then it will validate all response expectations.
// If allowDialError is set to true then function will not fail if connection is a dial error.
func RuntimeRequestWithExpectations(ctx context.Context, t *testing.T, client *http.Client, url string,
	responseExpectations []ResponseExpectation,
	allowDialError bool,
	opts ...RequestOption) *types.RuntimeInfo {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Error("Error creating Request:", err)
		return nil
	}

	for _, opt := range opts {
		opt(req)
	}

	resp, err := client.Do(req)

	if err != nil {
		if !allowDialError || !IsDialError(err) {
			t.Error("Error making GET request:", err)
		}
		return nil
	}

	defer resp.Body.Close()

	for _, e := range responseExpectations {
		if err := e(resp); err != nil {
			t.Error("Error meeting response expectations:", err)
			DumpResponse(ctx, t, resp)
			return nil
		}
	}

	if resp.StatusCode == http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Error("Unable to read response body:", err)
			DumpResponse(ctx, t, resp)
			return nil
		}
		ri := &types.RuntimeInfo{}
		if err := json.Unmarshal(b, ri); err != nil {
			t.Error("Unable to parse runtime image's response payload:", err)
			return nil
		}
		return ri
	}
	return nil
}

func DumpResponse(ctx context.Context, t *testing.T, resp *http.Response) {
	t.Helper()
	b, err := httputil.DumpResponse(resp, true)
	if err != nil {
		t.Error("Error dumping response:", err)
	}
	t.Log(string(b))
}

func StatusCodeExpectation(statusCodes sets.Int) ResponseExpectation {
	return func(response *http.Response) error {
		if !statusCodes.Has(response.StatusCode) {
			return fmt.Errorf("got unexpected status: %d, expected %v", response.StatusCode, statusCodes)
		}
		return nil
	}
}

func IsDialError(err error) bool {
	var errNetOp *net.OpError
	if !errors.As(err, &errNetOp) {
		return false
	}
	return errNetOp.Op == "dial"
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
func CreateDialContext(ctx context.Context, t *testing.T, ing *v1alpha1.Ingress, clients *test.Clients) func(context.Context, string, string) (net.Conn, error) {
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

	var svc *corev1.Service
	err := reconciler.RetryTestErrors(func(attempts int) (err error) {
		svc, err = clients.KubeClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("Unable to retrieve Kubernetes service %s/%s: %v", namespace, name, err)
	}

	var dialBackoff = wait.Backoff{
		Duration: 50 * time.Millisecond,
		Factor:   1.4,
		Jitter:   0.1, // At most 10% jitter.
		Steps:    100,
		Cap:      10 * time.Second,
	}

	dial := network.NewBackoffDialer(dialBackoff)
	if pkgTest.Flags.IngressEndpoint != "" {
		t.Logf("ingressendpoint: %q", pkgTest.Flags.IngressEndpoint)

		// If we're using a manual --ingressendpoint then don't require
		// "type: LoadBalancer", which may not play nice with KinD
		return func(ctx context.Context, _ string, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			for _, sp := range svc.Spec.Ports {
				if fmt.Sprint(sp.Port) == port {
					return dial(ctx, "tcp", fmt.Sprintf("%s:%d", pkgTest.Flags.IngressEndpoint, sp.NodePort))
				}
			}
			return nil, fmt.Errorf("service doesn't contain a matching port: %s", port)
		}
	} else if len(svc.Status.LoadBalancer.Ingress) >= 1 {
		ingress := svc.Status.LoadBalancer.Ingress[0]
		return func(ctx context.Context, _ string, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if ingress.IP != "" {
				return dial(ctx, "tcp", ingress.IP+":"+port)
			}
			if ingress.Hostname != "" {
				return dial(ctx, "tcp", ingress.Hostname+":"+port)
			}
			return nil, errors.New("service ingress does not contain dialing information")
		}
	} else {
		t.Fatal("Service does not have a supported shape (not type LoadBalancer? missing --ingressendpoint?).")
		return nil // Unreachable
	}
}
