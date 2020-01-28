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
	"testing"

	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	autotls "knative.dev/serving/test/e2e/autotls"
	v1test "knative.dev/serving/test/v1"
)

// To run this test locally with cert-manager, you need to
// 1. Install cert-manager from `third_party/` directory.
// 2. Run the command below to do the configuration:
// kubectl apply -f test/config/autotls/certmanager/selfsigned/
func TestPerKsvcCert_localCA(t *testing.T) {
	clients := e2e.Setup(t)
	autotls.DisableNamespaceCert(t, clients)

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
	defer cancel()

	// wait for certificate to be ready
	autotls.WaitForCertificateReady(t, clients, routenames.Certificate(objects.Route))

	// curl HTTPS
	secretName := routenames.Certificate(objects.Route)
	rootCAs := autotls.CreateRootCAs(t, clients, objects.Route.Namespace, secretName)
	httpsClient := autotls.CreateHTTPSClient(t, clients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}
