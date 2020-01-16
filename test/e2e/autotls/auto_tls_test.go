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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	pkgtest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/networking"
	cmclientset "knative.dev/serving/pkg/client/certmanager/clientset/versioned"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"

	"github.com/ghodss/yaml"
	cmv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
)

const (
	systemNamespace = "knative-serving"
)

var (
	caClusterIssuer = &cmv1alpha2.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ca-issuer",
		},
		Spec: cmv1alpha2.IssuerSpec{
			IssuerConfig: cmv1alpha2.IssuerConfig{
				CA: &cmv1alpha2.CAIssuer{},
			},
		},
	}
)

type autoTLSClients struct {
	clients  *test.Clients
	cmClient cmclientset.Interface
}

func TestPerKsvcCert_localCA(t *testing.T) {
	tlsClients := initializeClients(t)
	disableNamespaceCert(t, tlsClients)

	// Create Knative Service
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(tlsClients.clients, names) })
	objects, err := v1test.CreateServiceReady(t, tlsClients.clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// Create TLS certificate for the Knative Service.
	rootCAs := x509.NewCertPool()
	secretName, cancel := testingress.CreateTLSSecretWithCertPool(t, tlsClients.clients, []string{objects.Service.Status.URL.Host}, "cert-manager", rootCAs)
	defer cancel()

	// Create ClusterIssuer and update config-certmanager to reference the created ClusterIssuer
	clusterIssuer, cancel := createClusterIssuer(t, tlsClients, secretName)
	defer cancel()
	cancel = updateConfigCertManangerCM(t, tlsClients, clusterIssuer)
	defer cancel()

	cancel = turnOnAutoTLS(t, tlsClients)
	defer cancel()

	// wait for certificate to be ready
	waitForCertificateReady(t, tlsClients, routenames.Certificate(objects.Route))

	// curl HTTPS
	httpsClient := createHTTPSClient(t, tlsClients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

func createHTTPSClient(t *testing.T, tlsClients *autoTLSClients, objects *v1test.ResourceObjects, rootCAs *x509.CertPool) *http.Client {
	ing, err := tlsClients.clients.NetworkingClient.Ingresses.Get(routenames.Ingress(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Ingress %s: %v", routenames.Ingress(objects.Route), err)
	}
	dialer := testingress.CreateDialContext(t, ing, tlsClients.clients)
	tlsConfig := &tls.Config{
		RootCAs: rootCAs,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     dialer,
			TLSClientConfig: tlsConfig,
		}}
}

func initializeClients(t *testing.T) *autoTLSClients {
	clientConfig, err := test.BuildClientConfig(pkgtest.Flags.Kubeconfig, pkgtest.Flags.Cluster)
	if err != nil {
		t.Fatalf("Failed to create client config: %v.", err)
	}
	clients := &autoTLSClients{}
	clients.clients = e2e.Setup(t)
	clients.cmClient, err = cmclientset.NewForConfig(clientConfig)
	if err != nil {
		t.Fatalf("Failed to create cert manager client: %v", err)
	}
	return clients
}

func disableNamespaceCert(t *testing.T, tlsClients *autoTLSClients) {
	namespaces, err := tlsClients.clients.KubeClient.Kube.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}
	for _, ns := range namespaces.Items {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[networking.DisableWildcardCertLabelKey] = "true"
		if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().Namespaces().Update(&ns); err != nil {
			t.Errorf("Fail to disable namespace cert: %v", err)
		}
	}
}

func createClusterIssuer(t *testing.T, tlsClients *autoTLSClients, tlsSecretName string) (*cmv1alpha2.ClusterIssuer, context.CancelFunc) {
	copy := caClusterIssuer.DeepCopy()
	copy.Spec.CA.SecretName = tlsSecretName
	test.CleanupOnInterrupt(func() {
		tlsClients.cmClient.CertmanagerV1alpha2().ClusterIssuers().Delete(copy.Name, &metav1.DeleteOptions{})
	})
	if _, err := tlsClients.cmClient.CertmanagerV1alpha2().ClusterIssuers().Create(copy); err != nil {
		t.Fatalf("Failed to create ClusterIssuer %v: %v", &copy, err)
	}
	return copy, func() {
		if err := tlsClients.cmClient.CertmanagerV1alpha2().ClusterIssuers().Delete(copy.Name, &metav1.DeleteOptions{}); err != nil {
			t.Errorf("Failed to clean up ClusterIssuer %s: %v", copy.Name, err)
		}
	}
}

func updateConfigCertManangerCM(t *testing.T, tlsClients *autoTLSClients, clusterIssuer *cmv1alpha2.ClusterIssuer) context.CancelFunc {
	issuerRef := &cmmeta.ObjectReference{
		Name: clusterIssuer.Name,
		Kind: "ClusterIssuer",
	}
	issuerRefBytes, err := yaml.Marshal(issuerRef)
	if err != nil {
		t.Fatalf("Failed to convert IssuerRef %v to bytes: %v", issuerRef, err)
	}

	certManagerCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-certmanager", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get config-certmanager ConfigMap: %v", err)
	}
	certManagerCM.Data["issuerRef"] = string(issuerRefBytes)
	test.CleanupOnInterrupt(func() {
		cleanUpConfigCertManagerCM(t, tlsClients)
	})
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(certManagerCM.Namespace).Update(certManagerCM); err != nil {
		t.Fatalf("Failed to update the config-certmanager ConfigMap: %v", err)
	}
	return func() {
		cleanUpConfigCertManagerCM(t, tlsClients)
	}
}

func cleanUpConfigCertManagerCM(t *testing.T, tlsClients *autoTLSClients) {
	certManagerCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-certmanager", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get config-certmanager ConfigMap: %v", err)
		return
	}
	delete(certManagerCM.Data, "issuerRef")
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(certManagerCM.Namespace).Update(certManagerCM); err != nil {
		t.Errorf("Failed to clean up config-certmanager ConfigMap: %v", err)
	}
}

func turnOnAutoTLS(t *testing.T, tlsClients *autoTLSClients) context.CancelFunc {
	configNetworkCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get config-network ConfigMap: %v", err)
	}
	configNetworkCM.Data["autoTLS"] = "Enabled"
	test.CleanupOnInterrupt(func() {
		turnOffAutoTLS(t, tlsClients)
	})
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Update(configNetworkCM); err != nil {
		t.Fatalf("Failed to update config-network ConfigMap: %v", err)
	}
	return func() {
		turnOffAutoTLS(t, tlsClients)
	}
}

func turnOffAutoTLS(t *testing.T, tlsClients *autoTLSClients) {
	configNetworkCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get config-network ConfigMap: %v", err)
		return
	}
	delete(configNetworkCM.Data, "autoTLS")
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(configNetworkCM.Namespace).Update(configNetworkCM); err != nil {
		t.Errorf("Failed to turn off Auto TLS: %v", err)
	}
}

func waitForCertificateReady(t *testing.T, tlsClients *autoTLSClients, certName string) {
	if err := wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
		cert, err := tlsClients.clients.NetworkingClient.Certificates.Get(certName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Certificate %s has not been created: %v", certName, err)
				return false, nil
			}
			return false, err
		}
		return cert.Status.IsReady(), nil
	}); err != nil {
		t.Fatalf("Certificate %s is not ready: %v", certName, err)
	}
}
