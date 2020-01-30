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
package nonhttp01

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	"knative.dev/serving/test/e2e/autotls"
	v1test "knative.dev/serving/test/v1"
)

// To run this test locally with cert-manager, you need to
// 1. Install cert-manager from `third_party/` directory.
// 2. Run the command below to do the configuration:
// kubectl apply -f test/config/autotls/certmanager/selfsigned/
func TestPerKsvcCertLocalCA(t *testing.T) {
	clients := e2e.Setup(t)
	autotls.DisableNamespaceCertWithWhiteList(t, clients, sets.String{})

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	cancel := autotls.TurnOnAutoTLS(t, clients)
	test.CleanupOnInterrupt(cancel)
	defer cancel()

	// wait for certificate to be ready
	autotls.WaitForCertificateReady(t, clients, routenames.Certificate(objects.Route))

	// curl HTTPS
	secretName := routenames.Certificate(objects.Route)
	rootCAs := autotls.CreateRootCAs(t, clients, objects.Route.Namespace, secretName)

	// The TLS info is added to the ingress after the service is created, that's
	// why we need to wait again
	err = v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady")
	if err != nil {
		t.Fatalf("Service %s did not become ready: %v", names.Service, err)
	}

	httpsClient := autotls.CreateHTTPSClient(t, clients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

// To run this test locally with cert-manager, you need to
// 1. Install cert-manager from `third_party/` directory.
// 2. Run the command below to do the configuration:
// kubectl apply -f test/config/autotls/certmanager/selfsigned/
func TestPerNamespaceCertLocalCA(t *testing.T) {
	clients := e2e.Setup(t)
	autotls.DisableNamespaceCertWithWhiteList(t, clients, sets.NewString(test.ServingNamespace))
	defer autotls.DisableNamespaceCertWithWhiteList(t, clients, sets.String{})

	cancel := autotls.TurnOnAutoTLS(t, clients)
	test.CleanupOnInterrupt(cancel)
	defer cancel()

	// wait for certificate to be ready
	certName, err := waitForNamespaceCertReady(clients)
	if err != nil {
		t.Fatalf("Namespace Cert failed to become ready: %v", err)
	}

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %s: %v", names.Service, err)
	}

	// curl HTTPS
	rootCAs := autotls.CreateRootCAs(t, clients, test.ServingNamespace, certName)
	httpsClient := autotls.CreateHTTPSClient(t, clients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

func waitForNamespaceCertReady(clients *test.Clients) (string, error) {
	var certName string
	err := wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
		certs, err := clients.NetworkingClient.Certificates.List(metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, cert := range certs.Items {
			if strings.Contains(cert.Name, test.ServingNamespace) {
				certName = cert.Name
				return cert.Status.IsReady(), nil
			}
		}
		// Namespace certificate has not been created.
		return false, nil
	})
	return certName, err
}
