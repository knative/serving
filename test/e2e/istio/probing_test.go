// +build e2e istio

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

package e2e

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

var (
	namespace = "knative-serving"
)

func TestIstioProbing(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := e2e.Setup(t)

	// Save the current Gateway to restore it after the test
	oldGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(v1a1test.Namespace).Get(v1a1test.GatewayName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Gateway %s/%s", v1a1test.Namespace, v1a1test.GatewayName)
	}
	test.CleanupOnInterrupt(func() { v1a1test.RestoreGateway(t, clients, *oldGateway) })
	defer v1a1test.RestoreGateway(t, clients, *oldGateway)

	// Create a dummy service to get the domain name
	var domain string
	func() {
		names := test.ResourceNames{
			Service: test.ObjectNameForTest(t),
			Image:   "helloworld",
		}
		test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
		defer test.TearDown(clients, names)
		objects, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
			false)
		if err != nil {
			t.Fatalf("Failed to create Service %s: %v", names.Service, err)
		}
		domain = strings.SplitN(objects.Route.Status.URL.Host, ".", 2)[1]
	}()

	tlsOptions := &istiov1alpha3.Server_TLSOptions{
		Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
		PrivateKey:        "/etc/istio/ingressgateway-certs/tls.key",
		ServerCertificate: "/etc/istio/ingressgateway-certs/tls.crt",
	}

	cases := []struct {
		name    string
		servers []*istiov1alpha3.Server
		urls    []string
	}{{
		name: "HTTP",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}},
		urls: []string{"http://%s/"},
	}, {
		name: "HTTP2",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http2",
				Number:   80,
				Protocol: "HTTP2",
			},
		}},
		urls: []string{"http://%s/"},
	}, {
		name: "HTTP custom port",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-http",
				Number:   443,
				Protocol: "HTTP",
			},
		}},
		urls: []string{"http://%s:443/"},
	}, {
		name: "HTTP & HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}, {
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"http://%s/", "https://%s/"},
	}, {
		name: "HTTP redirect & HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}, {
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"http://%s/", "https://%s/"},
	}, {
		name: "HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"https://%s/"},
	}, {
		name: "HTTPS non standard port",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-https",
				Number:   80,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"https://%s:80/"},
	}, {
		name: "unsupported protocol (GRPC)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-grpc",
				Number:   80,
				Protocol: "GRPC",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "unsupported protocol (TCP)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-tcp",
				Number:   80,
				Protocol: "TCP",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "unsupported protocol (Mongo)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-mongo",
				Number:   80,
				Protocol: "Mongo",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "port not present in service",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-http",
				Number:   8090,
				Protocol: "HTTP",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}

			// Create the service and wait for it to be ready
			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
			defer test.TearDown(clients, names)
			_, httpsTransportOption, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, true)
			if err != nil {
				t.Fatalf("Failed to create Service %s: %v", names.Service, err)
			}

			var opt interface{}
			if httpsTransportOption != nil && hasHTTPS(c.servers) {
				t.Fatalf("Https transport option is nil for servers %#v", c.servers)
			}
			opt = *httpsTransportOption

			// Probe the Service on all endpoints
			var g errgroup.Group
			for _, tmpl := range c.urls {
				tmpl := tmpl
				g.Go(func() error {
					u, err := url.Parse(fmt.Sprintf(tmpl, names.Service+"."+domain))
					if err != nil {
						return fmt.Errorf("failed to parse URL: %w", err)
					}
					if _, err := pkgTest.WaitForEndpointStateWithTimeout(
						clients.KubeClient,
						t.Logf,
						u,
						v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloWorldText))),
						"HelloWorldServesText",
						test.ServingFlags.ResolvableDomain,
						1*time.Minute,
						opt); err != nil {
						return fmt.Errorf("failed to probe %s: %w", u, err)
					}
					return nil
				})
			}
			err = g.Wait()
			if err != nil {
				t.Fatalf("Failed to probe the Service: %v", err)
			}
		})
	}
}

func hasHTTPS(servers []*istiov1alpha3.Server) bool {
	for _, server := range servers {
		if server.Port.Protocol == "HTTPS" {
			return true
		}
	}
	return false
}
