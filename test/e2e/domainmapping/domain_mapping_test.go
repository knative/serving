//go:build e2e
// +build e2e

/*
Copyright 2021 The Knative Authors

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

package domainmapping

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"testing"
	"time"

	"knative.dev/networking/pkg/apis/networking"

	"knative.dev/pkg/test/spoof"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestBYOCertificate(t *testing.T) {
	if !test.ServingFlags.EnableBetaFeatures {
		t.Skip("Beta features not enabled")
	}
	t.Parallel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	test.EnsureTearDown(t, clients, &names)

	ctx := context.Background()

	ksvc, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	host := ksvc.Service.Name + ".example.org"

	resolvableCustomDomain := false

	if test.ServingFlags.CustomDomain != "" {
		host = ksvc.Service.Name + "." + test.ServingFlags.CustomDomain
		resolvableCustomDomain = true
	}

	cert, key := makeCertificateForDomain(t, host)
	secret, err := clients.KubeClient.CoreV1().Secrets(test.ServingFlags.TestNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: test.AppendRandomString("byocert-secret"),
			Labels: map[string]string{
				networking.CertificateUIDLabelKey: "byocert-secret",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}, metav1.CreateOptions{})

	if err != nil {
		t.Fatalf("Secret creation could not be completed: %v", err)
	}

	test.EnsureCleanup(t, func() {
		err = clients.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Secret %s/%s could not be deleted: %v", secret.Namespace, secret.Name, err)
		}
	})

	dm := v1beta1.DomainMapping{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      host,
			Namespace: ksvc.Service.Namespace,
		},
		Spec: v1beta1.DomainMappingSpec{
			Ref: duckv1.KReference{
				APIVersion: "serving.knative.dev/v1",
				Name:       ksvc.Service.Name,
				Namespace:  ksvc.Service.Namespace,
				Kind:       "Service",
			},
			TLS: &v1beta1.SecretTLS{
				SecretName: secret.Name,
			}},
		Status: v1beta1.DomainMappingStatus{},
	}

	_, err = clients.ServingBetaClient.DomainMappings.Create(ctx, &dm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Problem creating DomainMapping %q: %v", host, err)
	}
	test.EnsureCleanup(t, func() {
		clients.ServingBetaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{})
	})

	err = wait.PollUntilContextTimeout(ctx, test.PollInterval, test.PollTimeout, true, func(context.Context) (bool, error) {
		dm, err := clients.ServingBetaClient.DomainMappings.Get(ctx, dm.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		ready := dm.Status.IsReady() && dm.Status.URL != nil && dm.Status.URL.Scheme == "https"
		return ready, nil
	})
	if err != nil {
		t.Fatalf("Polling for DomainMapping state produced an error: %v", err)
	}

	pemData := test.PemDataFromSecret(ctx, t.Logf, clients, secret.Namespace, secret.Name)
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(pemData) {
		t.Fatalf("Failed to add the certificate to the root CA")
	}

	var trustSelfSigned spoof.TransportOption = func(transport *http.Transport) *http.Transport {
		transport.TLSClientConfig = &tls.Config{
			RootCAs: rootCAs,
		}
		return transport
	}

	_, err = pkgTest.CheckEndpointState(ctx,
		clients.KubeClient,
		t.Logf,
		&url.URL{Scheme: "HTTPS", Host: host},
		spoof.MatchesBody(test.PizzaPlanetText1),
		"DomainMappingBYOSSLCert",
		resolvableCustomDomain,
		trustSelfSigned,
	)
	if err != nil {
		t.Fatalf("Service response unavailable: %v", err)
	}
}

func makeCertificateForDomain(t *testing.T, domainName string) (cert []byte, key []byte) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		t.Fatalf("Failed to generate serial number: %v", err)
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	public := &priv.PublicKey
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore: now,
		NotAfter:  now.Add(time.Hour * 12),

		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domainName},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, public, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}
	var certOut bytes.Buffer
	pem.Encode(&certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}
	var keyOut bytes.Buffer
	pem.Encode(&keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	return certOut.Bytes(), keyOut.Bytes()
}
