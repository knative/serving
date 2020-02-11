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
	"fmt"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	v1test "knative.dev/serving/test/v1"
)

// CreateRootCAs creates a CertPool from certificate Secret.
func CreateRootCAs(t *testing.T, clients *test.Clients, ns, secretName string) *x509.CertPool {
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

// CreateHTTPSClient creates a HTTPS clients for a given resource.
func CreateHTTPSClient(t *testing.T, clients *test.Clients, objects *v1test.ResourceObjects, rootCAs *x509.CertPool) *http.Client {
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

// DisableNamespaceCertWithWhiteList disables namespace certs except the whitelisted namespace.
func DisableNamespaceCertWithWhiteList(t *testing.T, clients *test.Clients, whiteLists sets.String) {
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

// TurnOnAutoTLS turns on Auto TLS feature.
func TurnOnAutoTLS(t *testing.T, clients *test.Clients) context.CancelFunc {
	configNetworkCM, err := clients.KubeClient.Kube.CoreV1().ConfigMaps(system.Namespace()).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get config-network ConfigMap: %v", err)
	}
	configNetworkCM.Data["autoTLS"] = "Enabled"
	test.CleanupOnInterrupt(func() {
		turnOffAutoTLS(t, clients)
	})
	if _, err := clients.KubeClient.Kube.CoreV1().ConfigMaps(system.Namespace()).Update(configNetworkCM); err != nil {
		t.Fatalf("Failed to update config-network ConfigMap: %v", err)
	}
	return func() {
		turnOffAutoTLS(t, clients)
	}
}

func turnOffAutoTLS(t *testing.T, clients *test.Clients) {
	configNetworkCM, err := clients.KubeClient.Kube.CoreV1().ConfigMaps(system.Namespace()).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get config-network ConfigMap: %v", err)
		return
	}
	delete(configNetworkCM.Data, "autoTLS")
	if _, err := clients.KubeClient.Kube.CoreV1().ConfigMaps(configNetworkCM.Namespace).Update(configNetworkCM); err != nil {
		t.Errorf("Failed to turn off Auto TLS: %v", err)
	}
}

// WaitForCertificateReady waits for Certificate to become ready.
func WaitForCertificateReady(t *testing.T, clients *test.Clients, certName string) {
	if err := wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
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
