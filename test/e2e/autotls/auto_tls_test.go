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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kelseyhightower/envconfig"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const dnsRecordDeadlineSec = 600 // 10 min

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
	AutoTLSTestName string `envconfig:"auto_tls_test_name" required: "false"`
}

var env config

// To run this E2E test locally:
// test case 1: testing per ksvc certificate provision with self-signed CA
//   1) `kubectl label namespace serving-tests networking.internal.knative.dev/disableWildcardCert=true`
//   2) `kubectl delete kcert --all -n serving-tests`
//   3) `kubectl apply -f test/config/autotls/certmanager/selfsigned/`
//   4) `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/autotls/... -run  ^TestAutoTLS$`
// test case 2: testing per namespace certificate provision with self-signed CA
//   1) `kubectl delete kcert --all -n serving-tests`
//   2) kubectl apply -f test/config/autotls/certmanager/selfsigned/
//   3) Run `kubectl edit namespace serving-tests` and remove the label networking.internal.knative.dev/disableWildcardCert
//   4) `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/autotls/... -run  ^TestAutoTLS$`
// test case 3: testing per ksvc certificate provision with HTTP challenge
//   1) `kubectl label namespace serving-tests networking.internal.knative.dev/disableWildcardCert=true`
//   2) `kubectl delete kcert --all -n serving-tests`
//   3) `kubectl apply -f test/config/autotls/certmanager/http01/`
//   4) `export SERVICE_NAME=http01`
//   5) `kubectl patch cm config-domain -n knative-serving -p '{"data":{"<your-custom-domain>":""}}'`
//   6) Add a DNS A record to map host `http01.serving-tests.<your-custom-domain>` to the Ingress IP.
//   7) `go test -v -tags=e2e -count=1 -timeout=600s ./test/e2e/autotls/... -run  ^TestAutoTLS$`
func TestAutoTLS(t *testing.T) {
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v.", err)
	}
	testName := "TestAutoTLS"
	if len(env.AutoTLSTestName) != 0 {
		testName = env.AutoTLSTestName
	}
	t.Run(testName, testAutoTLS)
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

	var certName string
	if isNamespaceCertEnabled(t, clients) {
		name, err := waitForNamespaceCertReady(clients)
		if err != nil {
			t.Fatalf("Failed to wait for namespace certificate to be ready: %v", err)
		}
		certName = name
	} else {
		certName = routenames.Certificate(objects.Route)
		if err := waitForCertificateReady(t, clients, certName); err != nil {
			t.Fatalf("Failed to wait for certificate %s to be ready: %v,", certName, err)
		}
	}

	// The TLS info is added to the ingress after the service is created, that's
	// why we need to wait again
	err = v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceReady, "ServiceIsReady")
	if err != nil {
		t.Fatalf("Service %s did not become ready: %v", names.Service, err)
	}

	// curl HTTPS
	rootCAs := createRootCAs(t, clients, objects.Route.Namespace, certName)
	httpsClient := createHTTPSClient(t, clients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

func isNamespaceCertEnabled(t *testing.T, clients *test.Clients) bool {
	t.Helper()
	ns, err := clients.KubeClient.Kube.CoreV1().Namespaces().Get(test.ServingNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get namespace %s: %v", test.ServingNamespace, err)
	}
	return ns.Labels[networking.DisableWildcardCertLabelKey] != "true"
}

func waitForCertificateReady(t *testing.T, clients *test.Clients, certName string) error {
	return wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
		cert, err := clients.NetworkingClient.Certificates.Get(certName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Certificate %s has not been created: %v", certName, err)
				return false, nil
			}
			return false, err
		}
		if cert.Status.GetCondition(v1alpha1.CertificateConditionReady).IsFalse() {
			return true, fmt.Errorf("certificate %s failed with status %v", cert.Name, cert.Status)
		}
		return cert.Status.IsReady(), nil
	})
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
				if cert.Status.GetCondition(v1alpha1.CertificateConditionReady).IsFalse() {
					return true, fmt.Errorf("certificate %s failed with status %v", cert.Name, cert.Status)
				}
				return cert.Status.IsReady(), nil
			}
		}
		// Namespace certificate has not been created.
		return false, nil
	})
	return certName, err
}

func createRootCAs(t *testing.T, clients *test.Clients, ns, secretName string) *x509.CertPool {
	t.Helper()
	secret, err := clients.KubeClient.Kube.CoreV1().Secrets(ns).Get(
		secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Secret %s: %v", secretName, err)
	}

	rootCAs, err := x509.SystemCertPool()
	if rootCAs == nil || err != nil {
		if err != nil {
			t.Logf("Failed to load cert poll from system: %v. Will create a new cert pool.", err)
		}
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(secret.Data[corev1.TLSCertKey]) {
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
