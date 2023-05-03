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
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/certificates"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/serving"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const knativeCASecretName = "serving-certs-cluster-local-domain-ca"

var dnsVariants = []struct {
	name   string
	suffix string
}{
	{"fqdn", ""},
	{"short", ".cluster.local"},
	{"shortest", ".svc.cluster.local"},
}

var testNamespaceCASecretName = test.AppendRandomString("ca-secret")

// cluster-local-domain-tls enabled
// matrix with:
// - cert-manager
// - knative-selfsigned-issuer
// matrix with:
//   - net-kourier
//     ... later others
func TestClusterLocalDomainTLSClusterLocalVisibility(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !(strings.Contains(test.ServingFlags.IngressClass, "kourier")) {
		t.Skip("Skip this test for non-kourier ingress.")
	}

	t.Parallel()
	clients := test.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	ctx := context.Background()
	withInternalVisibility := rtesting.WithServiceLabel(netapi.VisibilityLabelKey, serving.VisibilityClusterLocal)
	t.Log("Creating a new service with cluster-local visibility")
	resources, err := v1test.CreateServiceReady(t, clients, &names, withInternalVisibility)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// After the service is created, we need to wait for the CA to be populated,
	// then copy the CA to the testing namespace for the ProxyImage to be used
	copyCASecretToTestNamespace(t, err, clients, ctx)
	os.Setenv("CA_CERT", testNamespaceCASecretName)

	svcUrl := resources.Route.Status.URL.URL()
	if svcUrl.Scheme != "https" {
		t.Fatalf("URL scheme of service %v was not https", names.Service)
	}

	// Check access via https on all cluster-local-domains
	for _, dns := range dnsVariants {
		helloworldURL := &url.URL{
			Scheme: svcUrl.Scheme,
			Host:   strings.TrimSuffix(svcUrl.Host, dns.suffix),
			Path:   svcUrl.Path,
		}
		t.Run(dns.name, func(t *testing.T) {
			t.Parallel()
			e2e.TestProxyToHelloworld(t, clients, helloworldURL, false, false)
		})
	}
}

func TestClusterLocalDomainTLSClusterExternalVisibility(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !(strings.Contains(test.ServingFlags.IngressClass, "kourier")) {
		t.Skip("Skip this test for non-kourier ingress.")
	}

	t.Parallel()
	clients := test.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	ctx := context.Background()

	t.Log("Creating a new service with external visibility")
	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// After the service is created, we need to wait for the CA to be populated,
	// then copy the CA to the testing namespace for the ProxyImage to be used
	copyCASecretToTestNamespace(t, err, clients, ctx)
	os.Setenv("CA_CERT", testNamespaceCASecretName)

	externalURL := resources.Route.Status.URL.URL()
	internalURL := resources.Route.Status.Address.URL

	if internalURL.Scheme != "https" {
		t.Fatalf("Internal URL scheme of service %v was not https", names.Service)
	}

	if externalURL.Scheme != "http" {
		t.Fatalf("External URL scheme of service %v was not http", names.Service)
	}

	// Check normal access on external domain
	t.Run("external-access", func(t *testing.T) {
		t.Parallel()
		e2e.TestProxyToHelloworld(t, clients, externalURL, false, true)
	})

	// Check access via https on all cluster-local-domains
	for _, dns := range dnsVariants {
		helloworldURL := &url.URL{
			Scheme: internalURL.Scheme,
			Host:   strings.TrimSuffix(internalURL.Host, dns.suffix),
			Path:   internalURL.Path,
		}
		t.Run(dns.name, func(t *testing.T) {
			t.Parallel()
			e2e.TestProxyToHelloworld(t, clients, helloworldURL, false, false)
		})
	}
}

func copyCASecretToTestNamespace(t *testing.T, err error, clients *test.Clients, ctx context.Context) {
	err = wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		caSecret, err := clients.KubeClient.CoreV1().Secrets(system.Namespace()).Get(ctx, knativeCASecretName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		// CA not yet populated
		if len(caSecret.Data[certificates.CertName]) == 0 {
			return false, nil
		}
		// Create a secret with the CAs public key contents, that is read by the httpproxy image
		secret, err := clients.KubeClient.CoreV1().Secrets(test.ServingFlags.TestNamespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespaceCASecretName,
			},
			Data: map[string][]byte{
				certificates.CaCertName: caSecret.Data[certificates.CertName],
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Could not create CA secret in test-namespace: %v", err)
		}
		test.EnsureCleanup(t, func() {
			err = clients.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{})
			if err != nil {
				// This error is allowed to occur, as we call this method twice
				t.Logf("Secret %s/%s could not be deleted: %v", secret.Namespace, secret.Name, err)
			}
		})
		return true, nil
	})
	if err != nil {
		t.Fatalf("Waiting for Knative selfsigned CA to be populated and copying it to test-namespace failed: %v", err)
	}
}

func TestClusterLocalDomainTLSCARotation(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !(strings.Contains(test.ServingFlags.IngressClass, "kourier")) {
		t.Skip("Skip this test for non-kourier ingress.")
	}

	clients := test.Setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new service with cluster-local visibility")
	ctx := context.Background()
	withInternalVisibility := rtesting.WithServiceLabel(netapi.VisibilityLabelKey, serving.VisibilityClusterLocal)
	resources, err := v1test.CreateServiceReady(t, clients, &names, withInternalVisibility)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// After the service is created, we need to wait for the CA to be populated,
	// then copy the CA to the testing namespace for the ProxyImage to be used
	copyCASecretToTestNamespace(t, err, clients, ctx)
	os.Setenv("CA_CERT", testNamespaceCASecretName)

	// Check access via https on all cluster-local-domains
	svcUrl := resources.Route.Status.URL.URL()
	for _, dns := range dnsVariants {
		helloworldURL := &url.URL{
			Scheme: svcUrl.Scheme,
			Host:   strings.TrimSuffix(svcUrl.Host, dns.suffix),
			Path:   svcUrl.Path,
		}
		t.Run(dns.name+"-old-ca", func(t *testing.T) {
			e2e.TestProxyToHelloworld(t, clients, helloworldURL, false, false)
		})
	}

	// Trigger a CA rotation by modifying the CA secret in SYSTEM_NAMESPACE
	caSecret, err := clients.KubeClient.CoreV1().Secrets(system.Namespace()).Get(ctx, knativeCASecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get existing Knative self-signed CA secret %s/%s: %v", system.Namespace(), knativeCASecretName, err)
	}

	// dropping the values will re-populate them and fire the reconciler for all KnativeCertificates
	caSecret.Data[certificates.CertName] = nil
	caSecret.Data[certificates.PrivateKeyName] = nil

	_, err = clients.KubeClient.CoreV1().Secrets(system.Namespace()).Update(ctx, caSecret, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Could not update CA secret %s/%s: %v", system.Namespace(), knativeCASecretName, err)
	}

	// Delete the old CA secret in the test namespace
	err = clients.KubeClient.CoreV1().Secrets(test.ServingFlags.TestNamespace).Delete(ctx, testNamespaceCASecretName, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Could not delete CA secret %s/%s: %v", test.ServingFlags.TestNamespace, testNamespaceCASecretName, err)
	}

	// Copy the new CA secret to the test namespace
	copyCASecretToTestNamespace(t, err, clients, ctx)

	// Re-run the access test via https on all cluster-local-domains
	// using the new CA to verify trust to the backing service
	for _, dns := range dnsVariants {
		helloworldURL := &url.URL{
			Scheme: svcUrl.Scheme,
			Host:   strings.TrimSuffix(svcUrl.Host, dns.suffix),
			Path:   svcUrl.Path,
		}
		t.Run(dns.name+"-new-ca", func(t *testing.T) {
			e2e.TestProxyToHelloworld(t, clients, helloworldURL, false, false)
		})
	}
}
