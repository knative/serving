// +build e2e

/*
Copyright 2020 The Knative Authors

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
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"

	"github.com/kelseyhightower/envconfig"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/networking/pkg/apis/networking"
	ntest "knative.dev/networking/test"
	"knative.dev/networking/test/conformance/ingress"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/spoof"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

type config struct {
	// ServiceName is the name of testing Knative Service.
	// It is not required for self-signed CA or for the HTTP01 challenge when wildcard domain
	// is mapped to the Ingress IP.
	TLSServiceName string `envconfig:"tls_service_name" required:"false"`
	// AutoTLSTestName is the name of the auto tls. It is not required for local test.
	AutoTLSTestName string `envconfig:"auto_tls_test_name" required:"false" default:"TestAutoTLS"`
}

var env config

// Technically TLS is proper Go casing for acronyms, but it makes our test naming
// hyphenate between every character, and we hit length problems.
func TestTls(t *testing.T) {
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v.", err)
	}
	t.Run(env.AutoTLSTestName, testAutoTLS)
}

func TestTlsDisabledWithAnnotation(t *testing.T) {
	clients := e2e.SetupWithNamespace(t, test.TLSNamespace)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	test.EnsureTearDown(t, clients, &names)

	objects, err := v1test.CreateServiceReady(t, clients, &names, rtesting.WithServiceAnnotations(map[string]string{networking.DisableAutoTLSAnnotationKey: "true"}))
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if err = v1test.WaitForRouteState(clients.ServingClient, names.Route, routeTLSDisabled, "RouteTLSDisabled"); err != nil {
		t.Fatalf("Traffic for route: %s does not have TLS disabled: %v", names.Route, err)
	}

	if err = v1test.WaitForRouteState(clients.ServingClient, names.Route, routeURLHTTP, "RouteURLIsHTTP"); err != nil {
		t.Fatalf("Traffic for route: %s is not HTTP: %v", names.Route, err)
	}

	httpClient := createHTTPClient(t, clients, objects)
	runtimeRequest(t, httpClient, objects.Route.Status.URL.String())
}

func testAutoTLS(t *testing.T) {
	clients := e2e.SetupWithNamespace(t, test.TLSNamespace)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	if len(env.TLSServiceName) != 0 {
		names.Service = env.TLSServiceName
	}
	test.EnsureTearDown(t, clients, &names)

	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// The TLS info is added to the ingress after the service is created, that's
	// why we need to wait again
	err = v1test.WaitForServiceState(clients.ServingClient, names.Service, httpsReady, "HTTPSIsReady")
	if err != nil {
		t.Fatalf("Service %s did not become ready or have HTTPS URL: %v", names.Service, err)
	}

	// curl HTTPS
	certName := getCertificateName(t, clients, objects)
	rootCAs := createRootCAs(t, clients, objects.Route.Namespace, certName)
	httpsClient := createHTTPSClient(t, clients, objects, rootCAs)
	runtimeRequest(t, httpsClient, objects.Service.Status.URL.String())

	t.Run("Tag route", func(t *testing.T) {
		// Probe main URL while we update the route
		var transportOption spoof.TransportOption = func(transport *http.Transport) *http.Transport {
			transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
			return transport
		}

		if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, servingv1.RouteSpec{
			Traffic: []servingv1.TrafficTarget{{
				Tag:            "r",
				Percent:        ptr.Int64(50),
				LatestRevision: ptr.Bool(true),
			}, {
				Tag:            "b",
				Percent:        ptr.Int64(50),
				LatestRevision: ptr.Bool(true),
			}},
		}); err != nil {
			t.Fatal("Failed to update Service route spec:", err)
		}
		if err = v1test.WaitForRouteState(clients.ServingClient, names.Route, routeTrafficHTTPS, "RouteTrafficIsHTTPS"); err != nil {
			t.Fatalf("Traffic for route: %s is not HTTPS: %v", names.Route, err)
		}

		ing, err := clients.NetworkingClient.Ingresses.Get(context.Background(), routenames.Ingress(objects.Route), metav1.GetOptions{})
		if err != nil {
			t.Fatal("Failed to get ingress:", err)
		}
		for _, tls := range ing.Spec.TLS {
			// Each new cert has to be added to the root pool so we can make requests.
			if !rootCAs.AppendCertsFromPEM(test.PemDataFromSecret(context.Background(), t.Logf, clients, tls.SecretNamespace, tls.SecretName)) {
				t.Fatal("Failed to add the certificate to the root CA")
			}
		}

		// Start prober after the new rootCA is added.
		prober := test.RunRouteProber(t.Logf, clients, objects.Service.Status.URL.URL(), transportOption)
		defer test.AssertProberDefault(t, prober)

		route, err := clients.ServingClient.Routes.Get(context.Background(), objects.Route.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal("Failed to get route:", err)
		}
		httpsClient := createHTTPSClient(t, clients, objects, rootCAs)
		for _, traffic := range route.Status.Traffic {
			runtimeRequest(t, httpsClient, traffic.URL.String())
		}
	})
}

func getCertificateName(t *testing.T, clients *test.Clients, objects *v1test.ResourceObjects) string {
	t.Helper()
	ing, err := clients.NetworkingClient.Ingresses.Get(context.Background(), routenames.Ingress(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Ingress %s: %v", routenames.Ingress(objects.Route), err)
	}
	if len(ing.Spec.TLS) == 0 {
		t.Fatalf("IngressTLS field in Ingress %s does not exist.", ing.Name)
	}
	return ing.Spec.TLS[0].SecretName
}

func routeTrafficHTTPS(route *servingv1.Route) (bool, error) {
	for _, tt := range route.Status.Traffic {
		if tt.URL.URL().Scheme != "https" {
			return false, nil
		}
	}
	return route.Status.IsReady(), nil
}

func routeURLHTTP(route *servingv1.Route) (bool, error) {
	return route.Status.URL.URL().Scheme == "http", nil
}

func routeTLSDisabled(route *servingv1.Route) (bool, error) {
	var cond = route.Status.GetCondition("CertificateProvisioned")
	return cond.Status == "True" && cond.Reason == "TLSNotEnabled", nil
}

func httpsReady(svc *servingv1.Service) (bool, error) {
	if ready, err := v1test.IsServiceReady(svc); err != nil {
		return ready, err
	} else if !ready {
		return false, nil
	} else {
		return svc.Status.URL.Scheme == "https", nil
	}
}

func createRootCAs(t *testing.T, clients *test.Clients, ns, secretName string) *x509.CertPool {
	t.Helper()
	pemData := test.PemDataFromSecret(context.Background(), t.Logf, clients, ns, secretName)

	rootCAs, err := x509.SystemCertPool()
	if rootCAs == nil || err != nil {
		if err != nil {
			t.Logf("Failed to load cert poll from system: %v. Will create a new cert pool.", err)
		}
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(pemData) {
		t.Fatal("Failed to add the certificate to the root CA")
	}
	return rootCAs
}

func createHTTPSClient(t *testing.T, clients *test.Clients, objects *v1test.ResourceObjects, rootCAs *x509.CertPool) *http.Client {
	t.Helper()
	ing, err := clients.NetworkingClient.Ingresses.Get(context.Background(), routenames.Ingress(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Ingress %s: %v", routenames.Ingress(objects.Route), err)
	}
	dialer := ingress.CreateDialContext(context.Background(), t, ing, &ntest.Clients{
		KubeClient: clients.KubeClient,
	})
	tlsConfig := &tls.Config{
		RootCAs: rootCAs,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     dialer,
			TLSClientConfig: tlsConfig,
		}}
}

func createHTTPClient(t *testing.T, clients *test.Clients, objects *v1test.ResourceObjects) *http.Client {
	t.Helper()
	ing, err := clients.NetworkingClient.Ingresses.Get(context.Background(), routenames.Ingress(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Ingress %s: %v", routenames.Ingress(objects.Route), err)
	}
	dialer := ingress.CreateDialContext(context.Background(), t, ing, &ntest.Clients{
		KubeClient: clients.KubeClient,
	})
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     dialer,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}
}
