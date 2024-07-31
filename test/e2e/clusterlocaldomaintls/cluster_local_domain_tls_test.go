//go:build e2e
// +build e2e

/*
Copyright 2023 The Knative Authors

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

package clusterlocaldomaintls

import (
	"net/url"
	"strings"
	"testing"

	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

var dnsVariants = []struct {
	name   string
	suffix string
}{
	{"fqdn", ""},
	{"short", ".cluster.local"},
	{"shortest", ".svc.cluster.local"},
}

func TestClusterLocalDomainTLSClusterLocalVisibility(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !strings.Contains(test.ServingFlags.IngressClass, "kourier") &&
		!strings.Contains(test.ServingFlags.IngressClass, "contour") {
		t.Skip("Skip this test for non-kourier/contour ingress.")
	}

	t.Parallel()
	clients := test.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	withInternalVisibility := rtesting.WithServiceLabel(netapi.VisibilityLabelKey, serving.VisibilityClusterLocal)
	t.Log("Creating a new service with cluster-local visibility")
	resources, err := v1test.CreateServiceReady(t, clients, &names, withInternalVisibility)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// After the service is created, we need to wait for the CA to be populated,
	// then use that secret in the ProxyImage to trust the cluster-local https connection
	secret, err := e2e.GetCASecret(clients)
	if err != nil {
		t.Fatal(err.Error())
	}

	svcURL := resources.Route.Status.URL.URL()
	if svcURL.Scheme != "https" {
		t.Fatalf("URL scheme of service %v was not https", names.Service)
	}

	// Check access via https on all cluster-local-domains
	for _, dns := range dnsVariants {
		helloworldURL := &url.URL{
			Scheme: svcURL.Scheme,
			Host:   strings.TrimSuffix(svcURL.Host, dns.suffix),
			Path:   svcURL.Path,
		}
		t.Run(dns.name, func(t *testing.T) {
			e2e.TestProxyToHelloworld(t, clients, helloworldURL, false, false, secret)
		})
	}
}

func TestClusterLocalDomainTLSClusterExternalVisibility(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !strings.Contains(test.ServingFlags.IngressClass, "kourier") &&
		!strings.Contains(test.ServingFlags.IngressClass, "contour") {
		t.Skip("Skip this test for non-kourier/contour ingress.")
	}

	t.Parallel()
	clients := test.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new service with external visibility")
	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// After the service is created, we need to wait for the CA to be populated,
	// then use that secret in the ProxyImage to trust the cluster-local https connection
	secret, err := e2e.GetCASecret(clients)
	if err != nil {
		t.Fatal(err.Error())
	}

	externalURL := resources.Route.Status.URL.URL()
	internalURL := resources.Route.Status.Address.URL

	if internalURL.Scheme != "https" {
		t.Fatalf("Internal URL scheme of service %v was not https", names.Service)
	}

	// On OpenShift this is always https
	if externalURL.Scheme != "https" {
		t.Fatalf("External URL scheme of service %v was not https", names.Service)
	}

	// Check normal access on external domain
	t.Run("external-access", func(t *testing.T) {
		e2e.TestProxyToHelloworld(t, clients, externalURL, false, true, secret)
	})

	// Check access via https on all cluster-local-domains
	for _, dns := range dnsVariants {
		helloworldURL := &url.URL{
			Scheme: internalURL.Scheme,
			Host:   strings.TrimSuffix(internalURL.Host, dns.suffix),
			Path:   internalURL.Path,
		}
		t.Run(dns.name, func(t *testing.T) {
			e2e.TestProxyToHelloworld(t, clients, helloworldURL, false, false, secret)
		})
	}
}
