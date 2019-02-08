// +build e2e

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

package conformance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type protocolsTest struct {
	t       *testing.T
	logger  *logging.BaseLogger
	clients *test.Clients
	names   test.ResourceNames
}

func (pt *protocolsTest) setup(t *testing.T) {
	pt.t = t

	pt.clients = setup(t)
	pt.logger = logging.GetContextLogger(t.Name())
	pt.names = test.ResourceNames{
		Service: test.AppendRandomString("protocols", pt.logger),
		Image:   protocols,
	}

	test.CleanupOnInterrupt(func() { pt.teardown() }, pt.logger)
}

func (pt *protocolsTest) teardown() {
	tearDown(pt.clients, pt.names)
}

func (pt *protocolsTest) getProtocol(resp *spoof.Response) protocol {
	var got protocol

	err := json.Unmarshal(resp.Body, &got)
	if err != nil {
		pt.t.Fatalf("Can't unmarshal response %s: %v", resp.Body, err)
	}

	pt.logger.Infof("Parsed version: %q", got.String())

	return got
}

func (pt *protocolsTest) makeRequest(domain string) *spoof.Response {
	pt.logger.Infof("Making request to %q", domain)

	resp, err := pkgTest.WaitForEndpointState(
		pt.clients.KubeClient, pt.logger, domain,
		pkgTest.Retrying(
			func(resp *spoof.Response) (bool, error) {
				if resp.StatusCode == http.StatusOK {
					return true, nil
				}

				return true, fmt.Errorf("unexpected status: %d", resp.StatusCode)
			},
			http.StatusNotFound,
		),
		pt.t.Name(), test.ServingFlags.ResolvableDomain,
	)
	if err != nil {
		pt.t.Fatalf("Failed to get a successful request from %s: %v", domain, err)
	}

	pt.logger.Infof("Got response: %s", resp.Body)

	return resp
}

func (pt *protocolsTest) createService(options *test.Options) *v1alpha1.Service {
	pt.logger.Infof("Creating service %q with options: %#v", pt.names.Service, options)

	service := test.LatestService(test.ServingNamespace, pt.names, options)

	svc, err := pt.clients.ServingClient.Services.Create(service)
	if err != nil {
		pt.t.Fatalf("Error creating service: %s", err.Error())
	}

	if err := test.WaitForServiceState(pt.clients.ServingClient, pt.names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		pt.t.Fatalf("Service %s not marked Ready: %v", pt.names.Service, err)
	}

	pt.logger.Infof("Service %q created", pt.names.Service)

	return svc
}

func (pt *protocolsTest) getDomain(service *v1alpha1.Service) string {
	pt.names.Route = serviceresourcenames.Route(service)

	pt.logger.Infof("Fetching domain from route %q", pt.names.Route)

	if err := test.WaitForRouteState(pt.clients.ServingClient, pt.names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		pt.t.Fatalf("Route %s not marked Ready: %v", pt.names.Route, err)
	}

	route, err := pt.clients.ServingClient.Routes.Get(pt.names.Route, metav1.GetOptions{})
	if err != nil {
		pt.t.Fatalf("Error fetching Route %s: %v", pt.names.Route, err)
	}

	pt.logger.Infof("Got domain %q", route.Status.Domain)

	return route.Status.Domain
}

type protocol struct {
	Major int `json:"protoMajor"`
	Minor int `json:"protoMinor"`
}

func (p *protocol) String() string {
	return fmt.Sprintf("HTTP/%d.%d", p.Major, p.Minor)
}

func portOption(portname string) *test.Options {
	options := &test.Options{}

	if portname != "" {
		options.ContainerPorts = []corev1.ContainerPort{{Name: portname, ContainerPort: 8080}}
	}

	return options
}

func TestProtocols(t *testing.T) {
	tests := []struct {
		Name     string
		PortName string
		Want     protocol
	}{{
		Name:     "HTTP/2.0",
		PortName: "h2c",
		Want:     protocol{Major: 2, Minor: 0},
	}, {
		Name:     "HTTP/1.1",
		PortName: "http1",
		Want:     protocol{Major: 1, Minor: 1},
	}, {
		Name:     "Default",
		PortName: "",
		Want:     protocol{Major: 1, Minor: 1},
	}}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			var pt protocolsTest

			pt.setup(t)
			defer pt.teardown()

			options := portOption(tt.PortName)

			service := pt.createService(options)
			domain := pt.getDomain(service)

			response := pt.makeRequest(domain)
			got := pt.getProtocol(response)

			if got != tt.Want {
				t.Errorf("Want %s, got %s", tt.Want.String(), got.String())
			}
		})
	}
}
