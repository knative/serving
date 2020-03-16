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
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"

	"github.com/kelseyhightower/envconfig"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/networking"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

type dnsRecord struct {
	ip     string
	domain string
}

type config struct {
	// ServiceName is the name of testing Knative Service.
	// It is not required for self-signed CA or for the HTTP01 challenge when wildcard domain
	// is mapped to the Ingress IP.
	TLSServiceName string `envconfig:"tls_service_name" required: "false"`
	// AutoTLSTestName is the name of the auto tls. It is not required for local test.
	AutoTLSTestName string `envconfig:"auto_tls_test_name" required: "false" default:"TestAutoTLS"`
}

var env config

func TestAutoTLS(t *testing.T) {
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v.", err)
	}
	t.Run(env.AutoTLSTestName, testAutoTLS)
}

func testAutoTLS(t *testing.T) {
	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	if len(env.TLSServiceName) != 0 {
		names.Service = env.TLSServiceName
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

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
	testingress.RuntimeRequest(t, httpsClient, objects.Service.Status.URL.String())

	t.Run("Tag route", func(t *testing.T) {
		// Probe main URL while we update the route
		var transportOption spoof.TransportOption = func(transport *http.Transport) *http.Transport {
			transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
			return transport
		}
		prober := test.RunRouteProber(t.Logf, clients, objects.Service.Status.URL.URL(), transportOption)
		defer test.AssertProberDefault(t, prober)

		if _, err := v1test.UpdateServiceRouteSpec(t, clients, names, servingv1.RouteSpec{
			Traffic: []servingv1.TrafficTarget{{
				Tag:            "tag1",
				Percent:        ptr.Int64(50),
				LatestRevision: ptr.Bool(true),
			}, {
				Tag:            "tag2",
				Percent:        ptr.Int64(50),
				LatestRevision: ptr.Bool(true),
			}},
		}); err != nil {
			t.Fatalf("Failed to update Service route spec: %v", err)
		}
		if err = v1test.WaitForRouteState(clients.ServingClient, names.Route, routeTrafficHTTPS, "RouteTrafficIsHTTPS"); err != nil {
			t.Fatalf("Traffic for route: %s is not HTTPS: %v", names.Route, err)
		}

		ing, err := clients.NetworkingClient.Ingresses.Get(routenames.Ingress(objects.Route), metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get ingress: %v", err)
		}
		for _, tls := range ing.Spec.TLS {
			// Each new cert has to be added to the root pool so we can make requests.
			if !rootCAs.AppendCertsFromPEM(test.PemDataFromSecret(t.Logf, clients, tls.SecretNamespace, tls.SecretName)) {
				t.Fatal("Failed to add the certificate to the root CA")
			}
		}

		route, err := clients.ServingClient.Routes.Get(objects.Route.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get route: %v", err)
		}
		httpsClient := createHTTPSClient(t, clients, objects, rootCAs)
		for _, traffic := range route.Status.Traffic {
			testingress.RuntimeRequest(t, httpsClient, traffic.URL.String())
		}
	})
}

func getCertificateName(t *testing.T, clients *test.Clients, objects *v1test.ResourceObjects) string {
	t.Helper()
	ing, err := clients.NetworkingClient.Ingresses.Get(routenames.Ingress(objects.Route), metav1.GetOptions{})
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
	return route.Status.IsReady() && true, nil
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
	pemData := test.PemDataFromSecret(t.Logf, clients, ns, secretName)

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
	ing, err := clients.NetworkingClient.Ingresses.Get(routenames.Ingress(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Ingress %s: %v", routenames.Ingress(objects.Route), err)
	}
	dialer := testingress.CreateDialContext(t, ing, clients)
	tlsConfig := &tls.Config{
		RootCAs: rootCAs,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     dialer,
			TLSClientConfig: tlsConfig,
		}}
}

func disableNamespaceCertWithWhiteList(t *testing.T, clients *test.Clients, whiteLists sets.String) {
	t.Helper()
	namespaces, err := clients.KubeClient.Kube.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}
	for _, ns := range namespaces.Items {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		if whiteLists.Has(ns.Name) {
			delete(ns.Labels, networking.DisableWildcardCertLabelKey)
		} else {
			ns.Labels[networking.DisableWildcardCertLabelKey] = "true"
		}
		if _, err := clients.KubeClient.Kube.CoreV1().Namespaces().Update(&ns); err != nil {
			t.Errorf("Fail to disable namespace cert: %v", err)
		}
	}
}
